package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/lmctl-ai/lmmock/internal/wire"
	_ "github.com/lmctl-ai/lmmock/internal/wire/grpcwire"
	_ "github.com/lmctl-ai/lmmock/internal/wire/h2"
	_ "github.com/lmctl-ai/lmmock/internal/wire/line"
	_ "github.com/lmctl-ai/lmmock/internal/wire/ws"
)

func wireCommand(ctx context.Context, args []string) int {
	if len(args) == 3 && args[1] == "--lmcode-schema" && args[2] == "--json" {
		return wireCommand(ctx, []string{"schema", args[0]})
	}
	if len(args) == 2 && args[1] == "--lmcode-signatures" {
		lines, err := wire.Signatures(args[0])
		if err != nil {
			return fail(err)
		}
		for _, line := range lines {
			fmt.Println(line)
		}
		return 0
	}

	if len(args) == 1 && args[0] == "--list" {
		for _, name := range wire.Names() {
			fmt.Println(name)
		}
		return 0
	}
	if len(args) == 2 && args[0] == "schema" {
		b, err := wire.Schema(args[1])
		if err != nil {
			return fail(err)
		}
		_, err = os.Stdout.Write(b)
		if err != nil {
			return fail(err)
		}
		return 0
	}
	if len(args) < 2 || (args[0] != "serve" && args[0] != "exec") {
		fmt.Fprintln(os.Stderr, "usage: lmmock wire schema KIND | wire serve|exec KIND --config SCENARIO [--listen IP:port] [-- COMMAND]")
		return 2
	}
	mode, kind := args[0], args[1]
	flags := flag.NewFlagSet("wire "+mode, flag.ContinueOnError)
	file := flags.String("config", "", "scenario with program, protocol and data fields")
	address := flags.String("listen", "127.0.0.1:0", "loopback listener")
	if err := flags.Parse(args[2:]); err != nil {
		return 2
	}
	command := flags.Args()
	if *file == "" || (mode == "exec" && len(command) == 0) || (mode == "serve" && len(command) > 0) {
		fmt.Fprintln(os.Stderr, "scenario required; exec requires a client command")
		return 2
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fail(errors.New("wire listen address must be a loopback IP"))
	}
	// Validate schema, descriptors, bindings and program before opening a port.
	engine, err := wire.Open(kind, *file)
	if err != nil {
		return fail(err)
	}
	ln, err := net.Listen("tcp", *address)
	if err != nil {
		return fail(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	url := engine.Scheme() + "://" + ln.Addr().String()
	done := make(chan error, 1)
	go func() { done <- engine.Serve(ctx, ln) }()
	if mode == "serve" {
		if err = json.NewEncoder(os.Stdout).Encode(map[string]any{"url": url, "address": ln.Addr().String(), "protocol": kind, "pid": os.Getpid()}); err != nil {
			cancel()
			<-done
			return fail(err)
		}
		err = <-done
		if err != nil && ctx.Err() == nil {
			return fail(err)
		}
		return 0
	}
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	env := []string{}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "LMMOCK_URL=") && !strings.HasPrefix(entry, "LMMOCK_ADDR=") {
			env = append(env, entry)
		}
	}
	child.Env = append(env, "LMMOCK_URL="+url, "LMMOCK_ADDR="+ln.Addr().String())
	child.Stdin = os.Stdin
	child.Stdout = os.Stdout
	child.Stderr = os.Stderr
	err = child.Run()
	cancel()
	serverErr := <-done
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		return fail(err)
	}
	if serverErr != nil {
		return fail(serverErr)
	}
	return 0
}
