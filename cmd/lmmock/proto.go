package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/lmctl-ai/lmmock/internal/proto"
)

// protoCommand runs a registered non-HTTP protocol mock server:
//
//	lmmock proto --list
//	lmmock proto <name> [--listen 127.0.0.1:0] [--config cfg.json] [--ready-file ready.json]
//
// It prints readiness JSON on stdout and blocks until SIGINT/SIGTERM.
func protoCommand(ctx context.Context, args []string) int {
	if len(args) == 1 && args[0] == "--list" {
		for _, name := range proto.Names() {
			fmt.Println(name)
		}
		return 0
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		fmt.Fprintln(os.Stderr, "usage: lmmock proto <name> [--listen IP:port] [--config cfg.json] [--ready-file f] | lmmock proto --list")
		return 2
	}
	name := args[0]
	factory, ok := proto.Lookup(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "lmmock: unknown protocol %q (registered: %s)\n", name, strings.Join(proto.Names(), ", "))
		return 2
	}
	f := flag.NewFlagSet("proto "+name, flag.ContinueOnError)
	listen := f.String("listen", "127.0.0.1:0", "loopback IP:port; 0 chooses a free port")
	config := f.String("config", "", "protocol-specific JSON configuration file")
	ready := f.String("ready-file", "", "write readiness JSON and remove it on exit")
	if err := f.Parse(args[1:]); err != nil {
		return 2
	}
	var cfg json.RawMessage
	if *config != "" {
		data, err := os.ReadFile(*config)
		if err != nil {
			return fail(err)
		}
		cfg = data
	}
	server, err := factory(cfg)
	if err != nil {
		return fail(fmt.Errorf("proto %s: %w", name, err))
	}
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return fail(err)
	}
	info := struct {
		URL      string `json:"url"`
		Address  string `json:"address"`
		Protocol string `json:"protocol"`
		PID      int    `json:"pid"`
	}{server.Scheme() + "://" + ln.Addr().String(), ln.Addr().String(), name, os.Getpid()}
	if *ready != "" {
		data, _ := json.Marshal(info)
		data = append(data, '\n')
		file, err := os.CreateTemp(filepath.Dir(*ready), ".lmmock-ready-")
		if err != nil {
			_ = ln.Close()
			return fail(err)
		}
		tmp := file.Name()
		defer os.Remove(tmp)
		_, err = file.Write(data)
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(tmp, *ready)
		}
		if err != nil {
			_ = ln.Close()
			return fail(err)
		}
		defer os.Remove(*ready)
	}
	if err = json.NewEncoder(os.Stdout).Encode(info); err != nil {
		_ = ln.Close()
		return fail(err)
	}
	if err = server.Serve(ctx, ln); err != nil && ctx.Err() == nil {
		return fail(fmt.Errorf("proto %s: %w", name, err))
	}
	return 0
}
