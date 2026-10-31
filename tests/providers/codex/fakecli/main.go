// Test-owned native executable: generic RPC transport only; policy is the embedded lmcode preset.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/lmctl-ai/lmmock/internal/rpcstdio"
)

func main() {
	if len(os.Args) < 4 || os.Args[1] != "app-server" || os.Args[2] != "--listen" || os.Args[3] != "stdio://" {
		fmt.Fprintln(os.Stderr, "owned Codex fixture only supports app-server stdio")
		os.Exit(2)
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "owned Codex fixture:", err)
		os.Exit(1)
	}
}
func run() error {
	directory := os.Getenv("LMMOCK_CODEX_TRACE")
	if directory == "" {
		return fmt.Errorf("owned trace directory required")
	}
	cfg, err := os.ReadFile(os.Getenv("LMMOCK_CODEX_CONFIG"))
	if err != nil {
		return err
	}
	input, err := os.OpenFile(filepath.Join(directory, "input.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(filepath.Join(directory, "output.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer output.Close()
	writers := []io.Writer{os.Stdout, output}
	// A test-owned observer receives the same ordered bytes as the real client.
	// Filesystem change notifications are not a reliable cross-platform rendezvous.
	if address := os.Getenv("LMMOCK_CODEX_OBSERVER_ADDR"); address != "" {
		host, _, err := net.SplitHostPort(address)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return fmt.Errorf("observer address must be loopback")
		}
		observer, err := net.Dial("tcp", address)
		if err != nil {
			return err
		}
		defer observer.Close()
		writers = append(writers, observer)
	}
	if err = os.WriteFile(filepath.Join(directory, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		return err
	}
	argv, _ := json.Marshal(os.Args[1:])
	_ = os.WriteFile(filepath.Join(directory, "argv.json"), argv, 0600)
	code, err := rpcstdio.Preset("codex")
	if err != nil {
		return err
	}
	// Signals close only this process's input. There is no child provider, sleep or native fallback.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = os.Stdin.Close()
		case <-done:
		}
	}()
	engine, err := rpcstdio.New(code, rpcstdio.Options{Alias: "Worker", Config: cfg}, rpcstdio.NewWire(io.TeeReader(os.Stdin, input), io.MultiWriter(writers...), "line"))
	if err != nil {
		return err
	}
	err = engine.Run()
	_ = os.WriteFile(filepath.Join(directory, "closed"), []byte("closed\n"), 0600)
	if ctx.Err() != nil {
		return nil
	}
	return err
}
