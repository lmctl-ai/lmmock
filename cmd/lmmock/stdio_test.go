package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStdioFailClosedPeerGuard(t *testing.T) {
	root := t.TempDir()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	if _, e = guardedCommand(exe, root); e == nil {
		t.Fatal("real executable outside fake-bin accepted")
	}
	if _, e = guardedCommand(exe, ""); e == nil {
		t.Fatal("missing fake-bin accepted")
	}
	inside := filepath.Join(root, "owned"+filepath.Ext(exe))
	if e = os.WriteFile(inside, []byte("#!/bin/sh\nexit 0\n"), 0700); e != nil {
		t.Fatal(e)
	}
	if _, e = guardedCommand(inside, root); e != nil {
		t.Fatal(e)
	}
	link := filepath.Join(root, "escape")
	if os.Symlink(exe, link) == nil {
		if _, e = guardedCommand(link, root); e == nil {
			t.Fatal("symlink escaped fake-bin")
		}
	}
}
func TestStdioUsageRefusals(t *testing.T) {
	for _, args := range [][]string{{}, {"unknown"}, {"agent", "--scenario", "x"}, {"acp", "--framing", "garbage", "--scenario", "x"}, {"mcp-client", "--scenario", "../../tests/stdio/scenario.json"}} {
		if stdioCommand(args) == 0 {
			t.Fatal("invalid options accepted", args)
		}
	}
}
