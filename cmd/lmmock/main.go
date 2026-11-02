package main

import (
	"bytes"
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
	"strings"
	"syscall"
	"time"

	lmcode "github.com/lmctl-ai/lmcode-go"
	"github.com/lmctl-ai/lmmock/internal/mock"
)

var version = "0.1.0"

func main() {
	args, err := processAlias(os.Args[1:])
	if err != nil {
		os.Exit(fail(err))
	}
	os.Exit(run(args))
}
func run(args []string) (exitCode int) {
	if os.Getenv("LMMOCK_JSONLINES_SCENARIO") != "" {
		return jsonLinesProcess(args)
	}
	if len(args) > 0 && args[0] == "jsonlines" {
		return jsonLinesCommand(args[1:])
	}
	if len(args) > 0 && args[0] == "stdio" {
		return stdioCommand(args[1:])
	}
	if len(args) > 0 && args[0] == "scenario" {
		return scenarioCommand(args[1:])
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) > 0 && args[0] == "sidecar" {
		return sidecarCommand(ctx, args[1:])
	}
	if len(args) > 0 && args[0] == "wire" {
		return wireCommand(ctx, args[1:])
	}
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
	dsl := f.String("lmcode", "", "private program file supplied by run_lmcode")
	builtin := f.String("builtin", "", "embedded mock program: llm-api, oauth-codex-device (no lmscript needed)")
	listen := f.String("listen", "127.0.0.1:0", "loopback IP:port; 0 chooses a free port")
	config := f.String("config", "", "trusted JSON configuration file, read by config()")
	ready := f.String("ready-file", "", "write readiness JSON and remove it on exit")
	upstream := f.String("upstream", "", "forward unmatched routes to this fixed http(s) base URL")
	upstreamTimeout := f.Duration("upstream-timeout", 2*time.Minute, "total upstream request timeout")
	upstreamReadOnly := f.Bool("upstream-read-only", false, "forward only GET/HEAD; reject other unmatched methods")
	auditLog := f.String("audit-log", "", "append mocked/forwarded request metadata as JSONL")
	requestLog := f.Bool("request-log", false, "record redacted request metadata for lmcode admin handlers")
	requestBodies := f.Bool("request-log-bodies", false, "also record scrubbed request bodies (implies --request-log)")
	tlsOptions := addTLSFlags(f)
	if err := f.Parse(args[1:]); err != nil {
		return lmcode.FailureFrame(os.Stdout, "cli", lmcode.NewError(lmcode.CLI_USAGE, "invalid execution flags"))
	}
	command := f.Args()
	if (*dsl == "") == (*builtin == "") || (mode == "exec" && len(command) == 0) || (mode == "serve" && len(command) > 0) {
		fmt.Fprintln(os.Stderr, "specify exactly one of --lmcode FILE or --builtin NAME; exec requires -- COMMAND")
		return lmcode.FailureFrame(os.Stdout, "cli", lmcode.NewError(lmcode.CLI_USAGE, "invalid execution arguments"))
	}
	// Embedded programs have a stable virtual registration; never ignore invocation
	// metadata, including its dry flag, just because no program file is read.
	registration := *dsl
	if *builtin != "" {
		var err error
		registration, err = filepath.Abs(filepath.Join("builtin", *builtin+".lmcode"))
		if err != nil {
			return lmcode.FailureFrame(os.Stdout, "cli", err)
		}
	}
	results, err := lmcode.InvocationResultsFromCommand([]string{registration})
	if err != nil {
		return lmcode.FailureFrame(os.Stdout, "cli", err)
	}
	phase := "load"
	var invocationErr error
	hostFailure := func(err error) int { invocationErr = err; return fail(err) }
	defer func() {
		if exitCode != 0 {
			if invocationErr == nil {
				invocationErr = lmcode.NewHostError(fmt.Sprintf("lmmock failed (exit %d)", exitCode))
			}
			if phase == "app" {
				invocationErr = lmcode.NewError(lmcode.APP_FAIL, fmt.Sprintf("client failed (exit %d): %v", exitCode, invocationErr))
			}
			_ = results.Fail(nil, phase, invocationErr)
		} else if err := results.Complete(0); err != nil {
			_ = results.Fail(nil, "run", err)
		}
		status := results.WriteFrame(os.Stdout)
		if os.Getenv("LMCODE_INVOCATION_FILE") != "" || exitCode == 0 || phase != "app" {
			exitCode = status
		}
	}()
	var code []byte
	if *builtin != "" {
		code, err = builtinProgram(*builtin)
	} else {
		code, err = os.ReadFile(*dsl)
	}
	if err != nil {
		return hostFailure(err)
	}
	var audit io.Writer
	if *auditLog != "" && !results.Dry() {
		file, err := os.OpenFile(*auditLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return hostFailure(err)
		}
		defer file.Close()
		audit = file
		fmt.Fprintln(os.Stderr, "lmmock audit log:", *auditLog)
	}
	engine, err := mock.NewWithInvocation(code, mock.Options{ConfigFile: *config, RequestLog: *requestLog, RequestLogBodies: *requestBodies, Upstream: *upstream,
		UpstreamTimeout: *upstreamTimeout, UpstreamReadOnly: *upstreamReadOnly, AuditWriter: audit}, results)
	if err != nil {
		var native *lmcode.Error
		if errors.As(err, &native) && strings.HasPrefix(native.Code, "RUN_") {
			phase = "run"
		}
		return hostFailure(err)
	}
	phase = "run"
	if results.Dry() {
		return 0
	}
	server, err := tlsOptions.start(engine, *listen)
	if err != nil {
		return hostFailure(err)
	}
	defer func() {
		if err := server.Close(); err != nil {
			fmt.Fprintln(os.Stderr, "lmmock shutdown:", err)
		}
	}()
	info := struct {
		URL    string `json:"url"`
		PID    int    `json:"pid"`
		CACert string `json:"ca_cert,omitempty"`
	}{server.URL, os.Getpid(), server.CACert}
	if *ready != "" {
		data, _ := json.Marshal(info)
		data = append(data, '\n')
		file, err := os.CreateTemp(filepath.Dir(*ready), ".lmmock-ready-")
		if err != nil {
			return hostFailure(err)
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
			return hostFailure(err)
		}
		defer os.Remove(*ready)
	}
	if mode == "serve" {
		if err = json.NewEncoder(os.Stdout).Encode(info); err != nil {
			return hostFailure(err)
		}
		<-ctx.Done()
		return 0
	}
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	child.Env = server.ClientEnv(os.Environ())
	// Invocation metadata belongs to this host, never to its unrelated client process.
	for i := len(child.Env) - 1; i >= 0; i-- {
		if strings.HasPrefix(child.Env[i], "LMCODE_INVOCATION_FILE=") {
			child.Env = append(child.Env[:i], child.Env[i+1:]...)
		}
	}
	child.Stdin = os.Stdin
	var clientOutput boundedOutput
	child.Stdout = os.Stdout
	if os.Getenv("LMCODE_INVOCATION_FILE") != "" {
		child.Stdout = &clientOutput
	}
	child.Stderr = os.Stderr
	phase = "app"
	err = child.Run()
	if ctx.Err() != nil {
		return 130
	}
	if err == nil {
		if os.Getenv("LMCODE_INVOCATION_FILE") != "" {
			if err := engine.SetClientOutput(clientOutput.String()); err != nil {
				return hostFailure(err)
			}
		}
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if exit.ExitCode() >= 0 {
			return exit.ExitCode()
		}
		return 1
	}
	return hostFailure(err)
}
func fail(err error) int { fmt.Fprintln(os.Stderr, "lmmock:", err); return 1 }
func usage() {
	fmt.Fprintln(os.Stderr, "       lmmock stdio acp|mcp|mcp-client|agent --scenario FILE (stdio --schema for handler declarations)")
	fmt.Fprintln(os.Stderr, "       lmmock wire schema KIND | wire serve|exec KIND --config SCENARIO [-- COMMAND]")
	fmt.Fprintln(os.Stderr, "       lmmock scenario import|scrub|verify --input FLOWS [--output FILE | --target URL]")
	_, _ = io.WriteString(os.Stderr, `lmmock - scriptable loopback HTTP mocks
  lmmock schema > schema.json
  lmmock serve --lmcode FILE [--listen 127.0.0.1:0] [--ready-file ready.json]
  lmmock exec --lmcode FILE -- COMMAND [ARG...]
  lmmock serve --builtin llm-api [--listen 127.0.0.1:0]
  lmmock exec --builtin llm-api [--config cfg.json] -- COMMAND [ARG...]
  serve/exec: [--tls] [--tls-host DNS_NAME] [--upstream http(s)://host/base/] [--upstream-timeout 2m]
              [--upstream-read-only] [--audit-log requests.jsonl]
  lmmock check --suite assertions.json [--url URL]
  lmmock proto --list
  lmmock proto <name> [--listen 127.0.0.1:0] [--config cfg.json] [--ready-file ready.json]
  lmmock --version
exec sets LMMOCK_URL for the client and propagates its exit code.
serve prints readiness JSON and stops on SIGINT/SIGTERM.
`)
}

// Captured client stdout is returned as data, never mistaken for a host frame.
type boundedOutput struct{ buffer bytes.Buffer }

func (b *boundedOutput) String() string { return b.buffer.String() }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.buffer.Len()+len(p) > 10*1024*1024 {
		return 0, errors.New("client stdout exceeds 10 MiB")
	}
	return b.buffer.Write(p)
}
