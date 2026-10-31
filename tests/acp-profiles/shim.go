// Test-only portable fake CLI. All provider policy lives in lmcode/config data.
package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

type traceWriter struct {
	mu        *sync.Mutex
	file      *os.File
	direction string
	pending   []byte
}

func (t *traceWriter) Write(b []byte) (int, error) {
	t.pending = append(t.pending, b...)
	for {
		p := strings.IndexByte(string(t.pending), '\n')
		if p < 0 {
			break
		}
		line := string(t.pending[:p])
		t.pending = t.pending[p+1:]
		t.mu.Lock()
		json.NewEncoder(t.file).Encode(map[string]string{"direction": t.direction, "line": line})
		t.mu.Unlock()
	}
	return len(b), nil
}
func main() {
	profile := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	dir := os.Getenv("LMMOCK_ACP_CASE_DIR")
	file, e := os.OpenFile(filepath.Join(dir, "trace.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		panic(e)
	}
	defer file.Close()
	var mu sync.Mutex
	json.NewEncoder(file).Encode(map[string]any{"direction": "spawn", "profile": profile, "argv": os.Args[1:], "hermes_yolo": os.Getenv("HERMES_YOLO_MODE")})
	child := exec.Command(os.Getenv("LMMOCK_TEST_BINARY"), "stdio", "acp", "--lmcode", os.Getenv("LMMOCK_ACP_BYTECODE"), "--scenario", filepath.Join(dir, "scenario.json"), "--alias", "Worker", "--")
	child.Args = append(child.Args, os.Args[1:]...)
	input, e := child.StdinPipe()
	if e != nil {
		panic(e)
	}
	child.Stdout = io.MultiWriter(&traceWriter{mu: &mu, file: file, direction: "server"}, os.Stdout)
	child.Stderr = os.Stderr
	if e = child.Start(); e != nil {
		panic(e)
	}
	go func() {
		_, _ = io.Copy(input, io.TeeReader(os.Stdin, &traceWriter{mu: &mu, file: file, direction: "client"}))
		_ = input.Close()
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-signals:
			_ = child.Process.Kill()
		case <-done:
		}
	}()
	e = child.Wait()
	_ = input.Close()
	close(done)
	signal.Stop(signals)
	if e != nil {
		if exit, ok := e.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		os.Exit(1)
	}
}
