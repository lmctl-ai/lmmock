package rpcstdio

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProcessStdoutStderrAndExit(t *testing.T) {
	code, err := os.ReadFile("testdata/process-output.dslb")
	if err != nil {
		t.Fatal("read protocol fixture", err)
	}
	var out, stderr bytes.Buffer
	cfg, _ := json.Marshal(map[string]any{"trace": filepath.Join(t.TempDir(), "trace.jsonl"), "request": map[string]string{}, "response": map[string]any{"stdout": "reply\n", "stderr": "diagnostic\n", "exit": 7}})
	e, err := New(code, Options{Config: cfg, Stderr: &stderr}, NewWire(bytes.NewReader(nil), &out, "line"))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Start(map[string]any{"argv": []string{}, "cwd": "fixture"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "reply\n" || stderr.String() != "diagnostic\n" || e.ExitCode() != 7 {
		t.Fatal("stdout/stderr/exit contract was lost")
	}
}
