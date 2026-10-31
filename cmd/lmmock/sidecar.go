package main

import (
	"context"
	"flag"
	"os"

	"github.com/lmctl-ai/lmmock/internal/mock"
	"github.com/lmctl-ai/lmmock/internal/rpcstdio"
)

// A sidecar combines the existing HTTP Handler and process/RPCHandler kinds.
// Boot spelling and protocol methods remain authored in lmcode.
func sidecarCommand(ctx context.Context, args []string) int {
	flags := flag.NewFlagSet("sidecar", flag.ContinueOnError)
	config := flags.String("config", "", "trusted fixture config")
	httpCode := flags.String("http-lmcode", "", "annotated HTTP Handler program")
	bootCode := flags.String("boot-lmcode", "", "annotated process startup program")
	if flags.Parse(args) != nil || *config == "" || *httpCode == "" || *bootCode == "" {
		return 2
	}
	raw, err := os.ReadFile(*config)
	if err != nil {
		return fail(err)
	}
	code, err := os.ReadFile(*httpCode)
	if err != nil {
		return fail(err)
	}
	engine, err := mock.NewWithOptions(code, mock.Options{ConfigFile: *config})
	if err != nil {
		return fail(err)
	}
	server, err := mock.Start(engine, "127.0.0.1:0")
	if err != nil {
		return fail(err)
	}
	defer server.Close()
	boot, err := os.ReadFile(*bootCode)
	if err != nil {
		return fail(err)
	}
	process, err := rpcstdio.New(boot, rpcstdio.Options{Config: raw, Stderr: os.Stderr}, rpcstdio.NewWire(os.Stdin, os.Stdout, "line"))
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	if err = process.Start(map[string]any{"argv": flags.Args(), "cwd": cwd, "url": server.URL, "pid": os.Getpid()}); err != nil {
		return fail(err)
	}
	if process.Stopped() {
		return process.ExitCode()
	}
	<-ctx.Done()
	return 0
}
