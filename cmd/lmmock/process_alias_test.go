package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProcessBindingsStayInsideOwnedDirectory(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(exe), ".exe")
	file := filepath.Join(t.TempDir(), "bindings.json")
	t.Setenv("LMMOCK_PROCESS_BINDINGS", file)
	value := map[string]any{"version": 1, "directory": filepath.Dir(exe), "aliases": map[string][]string{name: {"stdio", "once", "--"}}}
	write := func(extra string) {
		raw, _ := json.Marshal(value)
		if err = os.WriteFile(file, append(raw, []byte(extra)...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("")
	args, err := processAlias([]string{"--model", "fixture"})
	if err != nil || len(args) != 5 || args[4] != "fixture" {
		t.Fatal("owned alias argv not preserved", err)
	}
	value["directory"] = t.TempDir()
	write("")
	if _, err = processAlias(nil); err == nil {
		t.Fatal("fake process escaped owned executable directory")
	}
	value["directory"] = filepath.Dir(exe)
	write("garbage")
	if _, err = processAlias(nil); err == nil {
		t.Fatal("accepted trailing binding data")
	}
	value["unknown"] = true
	write("")
	if _, err = processAlias(nil); err == nil {
		t.Fatal("accepted unknown binding field")
	}
}
