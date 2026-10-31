package main

import (
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/rpcstdio"
	"os"
	"path/filepath"
)

// Explicit process injection: copying this executable under a client-selected
// name preserves native process semantics on Windows as well as Unix. This
// layer never recognizes a provider name, model, event or credential format.
func jsonLinesProcess(args []string) int {
	code, err := os.ReadFile(os.Getenv("LMMOCK_JSONLINES_PROGRAM"))
	if err != nil {
		return fail(err)
	}
	config, err := os.ReadFile(os.Getenv("LMMOCK_JSONLINES_SCENARIO"))
	if err != nil {
		return fail(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	alias := os.Getenv("LMMOCK_JSONLINES_ALIAS")
	if alias == "" {
		alias = filepath.Base(os.Args[0])
	}
	wire := rpcstdio.NewWire(os.Stdin, os.Stdout, "line")
	engine, err := rpcstdio.New(code, rpcstdio.Options{Config: config, Stderr: os.Stderr, Alias: alias, StateDir: os.Getenv("LMMOCK_JSONLINES_STATE")}, wire)
	if err != nil {
		return fail(err)
	}
	if err = engine.Start(map[string]any{"argv": args, "cwd": cwd}); err != nil {
		return fail(err)
	}
	if err = engine.RunJSONLines(); err != nil {
		return fail(err)
	}
	return engine.ExitCode()
}

func jsonLinesCommand(args []string) int {
	if len(args) == 1 && args[0] == "--schema" {
		_, err := os.Stdout.Write(rpcstdio.Schema())
		if err != nil {
			return fail(err)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "JSONL process injection requires explicit LMMOCK_JSONLINES_PROGRAM and LMMOCK_JSONLINES_SCENARIO in the owned child environment")
	return 2
}
