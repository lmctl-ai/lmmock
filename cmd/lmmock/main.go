package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

var version = "0.1.0"

func main() { os.Exit(run(os.Args[1:])) }
func run(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) > 0 && args[0] == "proto" {
		return protoCommand(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "check" {
		return checkCommand(args[1:])
	}
	if len(args) == 1 {
		switch args[0] {
		case "--version", "version":
			fmt.Println("lmmock " + version)
			return 0
		case "schema":
			_, _ = os.Stdout.Write(mock.Schema)
			return 0
		case "--help", "-h":
			usage()
			return 0
		}
	}
	if len(args) == 2 && args[0] == "--lmcode-schema" && args[1] == "--json" {
		_, _ = os.Stdout.Write(mock.Schema)
		return 0
	}
	if len(args) == 0 || (args[0] != "serve" && args[0] != "exec") {
		usage()
		return 2
	}
	mode := args[0]
	f := flag.NewFlagSet(mode, flag.ContinueOnError)
	script := f.String("script", "", "trusted .lms source (requires lmscript)")
	dsl := f.String("lmcode", "", "precompiled .dslb file")
	listen := f.String("listen", "127.0.0.1:0", "loopback IP:port; 0 chooses a free port")
	config := f.String("config", "", "trusted JSON configuration file, read by config()")
	ready := f.String("ready-file", "", "write readiness JSON and remove it on exit")
	upstream := f.String("upstream", "", "forward unmatched routes to this fixed http(s) base URL")
	upstreamTimeout := f.Duration("upstream-timeout", 2*time.Minute, "total upstream request timeout")
	upstreamReadOnly := f.Bool("upstream-read-only", false, "forward only GET/HEAD; reject other unmatched methods")
	auditLog := f.String("audit-log", "", "append mocked/forwarded request metadata as JSONL")
	if err := f.Parse(args[1:]); err != nil {
		return 2
	}
	command := f.Args()
	if (*script == "") == (*dsl == "") || (mode == "exec" && len(command) == 0) || (mode == "serve" && len(command) > 0) {
		fmt.Fprintln(os.Stderr, "specify exactly one of --script or --lmcode; exec requires -- COMMAND")
		return 2
	}
	if *script != "" {
		folder, err := os.MkdirTemp("", "lmmock-compile-")
		if err != nil {
			return fail(err)
		}
		defer os.RemoveAll(folder)
		*dsl = filepath.Join(folder, "mock.dslb")
		compiler := os.Getenv("LMSCRIPT_BIN")
		if compiler == "" {
			compiler = "lmscript"
		}
		cmd, err := compilerCommand(ctx, compiler, "--no-preflight", *script, *dsl)
		if err != nil {
			return fail(err)
		}
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err = cmd.Run(); err != nil {
			return fail(fmt.Errorf("compile source: %w", err))
		}
	}
	code, err := os.ReadFile(*dsl)
	if err != nil {
		return fail(err)
	}
	var audit io.Writer
	if *auditLog != "" {
		file, err := os.OpenFile(*auditLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return fail(err)
		}
		defer file.Close()
		audit = file
		fmt.Fprintln(os.Stderr, "lmmock audit log:", *auditLog)
	}
	engine, err := mock.NewWithOptions(code, mock.Options{ConfigFile: *config, Upstream: *upstream,
		UpstreamTimeout: *upstreamTimeout, UpstreamReadOnly: *upstreamReadOnly, AuditWriter: audit})
	if err != nil {
		return fail(err)
	}
	server, err := mock.Start(engine, *listen)
	if err != nil {
		return fail(err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "lmmock shutdown:", err)
		}
	}()
	info := struct {
		URL string `json:"url"`
		PID int    `json:"pid"`
	}{server.URL, os.Getpid()}
	if *ready != "" {
		data, _ := json.Marshal(info)
		data = append(data, '\n')
		file, err := os.CreateTemp(filepath.Dir(*ready), ".lmmock-ready-")
		if err != nil {
			return fail(err)
		}
		name := file.Name()
		defer os.Remove(name)
		_, err = file.Write(data)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(name, *ready)
		}
		if err != nil {
			return fail(err)
		}
		defer os.Remove(*ready)
	}
	if mode == "serve" {
		if err = json.NewEncoder(os.Stdout).Encode(info); err != nil {
			return fail(err)
		}
		<-ctx.Done()
		return 0
	}
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	for _, v := range os.Environ() {
		if !strings.EqualFold(strings.SplitN(v, "=", 2)[0], "LMMOCK_URL") {
			child.Env = append(child.Env, v)
		}
	}
	child.Env = append(child.Env, "LMMOCK_URL="+server.URL)
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	err = child.Run()
	if ctx.Err() != nil {
		return 130
	}
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		return 1
	}
	return fail(err)
}
func compilerCommand(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, fmt.Errorf("compiler %s: install npm -g @lmctl-ai/lmctl or use --lmcode: %w", name, err)
	}
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(path), ".cmd") {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		match := regexp.MustCompile(`(?i)%dp0%[\\/]([^"\r\n]+)"\s+%\*`).FindSubmatch(data)
		if len(match) != 2 {
			return nil, errors.New("unsupported npm compiler shim")
		}
		return exec.CommandContext(ctx, "node", append([]string{filepath.Join(filepath.Dir(path), string(match[1]))}, args...)...), nil
	}
	return exec.CommandContext(ctx, path, args...), nil
}
func fail(err error) int { fmt.Fprintln(os.Stderr, "lmmock:", err); return 1 }
func usage() {
	_, _ = io.WriteString(os.Stderr, `lmmock - scriptable loopback HTTP mocks
  lmmock schema > schema.json
  lmmock serve --script mock.lms [--listen 127.0.0.1:0] [--ready-file ready.json]
  lmmock exec --script mock.lms -- COMMAND [ARG...]
  lmmock exec --lmcode mock.dslb -- COMMAND [ARG...]
  serve/exec: [--upstream http(s)://host/base/] [--upstream-timeout 2m]
              [--upstream-read-only] [--audit-log requests.jsonl]
  lmmock check --suite assertions.json [--url URL]
  lmmock proto --list
  lmmock proto <name> [--listen 127.0.0.1:0] [--config cfg.json] [--ready-file ready.json]
  lmmock --version
exec sets LMMOCK_URL for the client and propagates its exit code.
serve prints readiness JSON and stops on SIGINT/SIGTERM.
`)
}
