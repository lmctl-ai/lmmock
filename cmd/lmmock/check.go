package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/check"
	"os"
)

func checkCommand(args []string) int {
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	base := flags.String("url", os.Getenv("LMMOCK_URL"), "server base URL (default LMMOCK_URL)")
	suite := flags.String("suite", "", "JSON assertion suite")
	if flags.Parse(args) != nil {
		return 2
	}
	if *suite == "" || len(flags.Args()) > 0 {
		fmt.Fprintln(os.Stderr, "usage: lmmock check --suite FILE [--url URL]")
		return 2
	}
	b, err := os.ReadFile(*suite)
	if err != nil {
		return fail(err)
	}
	var s check.Suite
	if err = json.Unmarshal(b, &s); err != nil {
		return fail(fmt.Errorf("invalid check suite"))
	}
	passed, err := check.Run(context.Background(), *base, s)
	result := map[string]any{"ok": err == nil, "passed": passed}
	if err != nil {
		result["error"] = err.Error()
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if err != nil {
		return 1
	}
	return 0
}
