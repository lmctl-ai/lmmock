package main

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func invocationOutput(t *testing.T, args []string) (int, string) {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), "stdout-")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = file
	defer func() { os.Stdout = previous; file.Close() }()
	code := run(args)
	b, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(b)
}
func invocationManifest(t *testing.T, program string, dry bool) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"version": 1, "mode": "single", "dry": dry, "parts": []any{map[string]any{"index": 0, "file": program}}})
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "invocation.json")
	if err = os.WriteFile(file, b, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LMCODE_INVOCATION_FILE", file)
}
func TestInvocationDryDoesNotStartClientOrCreateState(t *testing.T) {
	program := invocationProgram(t)
	invocationManifest(t, program, true)
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit")
	ready := filepath.Join(dir, "ready")
	code, out := invocationOutput(t, []string{"exec", "--lmcode", program, "--audit-log", audit, "--ready-file", ready, "--", "must-not-run"})
	if code != 0 || strings.Count(out, "LMCODE_RETURN_BEGIN") != 1 || !strings.Contains(out, `"dry":true`) {
		t.Fatalf("dry envelope: %d %s", code, out)
	}
	for _, file := range []string{audit, ready} {
		if _, err := os.Stat(file); !os.IsNotExist(err) {
			t.Fatalf("dry created state %s", file)
		}
	}
}
func TestInvocationFailuresHaveOneEnvelope(t *testing.T) {
	for _, load := range []bool{true, false} {
		t.Run(map[bool]string{true: "load", false: "app"}[load], func(t *testing.T) {
			program := invocationProgram(t)
			expected := 4
			phase := "app"
			if load {
				program = filepath.Join(t.TempDir(), "missing.lmcode")
				expected = 3
				phase = "load"
			}
			invocationManifest(t, program, false)
			code, out := invocationOutput(t, []string{"exec", "--lmcode", program, "--", "must-not-run"})
			if code != expected || strings.Count(out, "LMCODE_RETURN_BEGIN") != 1 || !strings.Contains(out, `"phase":"`+phase+`"`) {
				t.Fatalf("failure envelope: %d %s", code, out)
			}
		})
	}
}
func TestInvocationMetadataRefusedBeforeProgramRead(t *testing.T) {
	t.Setenv("LMCODE_INVOCATION_FILE", filepath.Join(t.TempDir(), "missing.json"))
	code, out := invocationOutput(t, []string{"exec", "--lmcode", "missing.lmcode", "--", "must-not-run"})
	if code != 2 || !strings.Contains(out, `"phase":"cli"`) {
		t.Fatalf("metadata: %d %s", code, out)
	}
}

func TestInvocationClientProcess(t *testing.T) {
	if os.Getenv("LMMOCK_INVOCATION_CLIENT") != "1" {
		return
	}
	if os.Getenv("LMCODE_INVOCATION_FILE") != "" {
		os.Exit(9)
	}
	_, _ = os.Stdout.WriteString("LMCODE_RETURN_BEGIN\nclient\nLMCODE_RETURN_END\n")
	os.Exit(0)
}
func TestInvocationClientStdoutIsDataNotAFrame(t *testing.T) {
	program := invocationProgram(t)
	invocationManifest(t, program, false)
	t.Setenv("LMMOCK_INVOCATION_CLIENT", "1")
	code, out := invocationOutput(t, []string{"exec", "--lmcode", program, "--", os.Args[0], "-test.run=^TestInvocationClientProcess$"})
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != 0 || len(lines) != 3 || lines[0] != "LMCODE_RETURN_BEGIN" || lines[2] != "LMCODE_RETURN_END" {
		t.Fatalf("frame: %d %s", code, out)
	}
	var frame struct {
		OK     bool `json:"ok"`
		Result struct {
			Stdout string `json:"stdout"`
			Code   int    `json:"code"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &frame); err != nil {
		t.Fatal(err)
	}
	if !frame.OK || frame.Result.Code != 0 || frame.Result.Stdout != "LMCODE_RETURN_BEGIN\nclient\nLMCODE_RETURN_END\n" {
		t.Fatalf("client output lost: %#v", frame)
	}
}
func TestCapturedOutputBound(t *testing.T) {
	var out boundedOutput
	if _, err := out.Write(make([]byte, 10*1024*1024)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&out, struct{ io.Reader }{strings.NewReader("x")}); err == nil {
		t.Fatal("unbounded output")
	}
}

func invocationProgram(t *testing.T) string {
	t.Helper()
	program := filepath.Join(t.TempDir(), "program.lmcode")
	cmd := exec.Command("node", "../../scripts/capture-fixture.mjs", "../../examples/api.lms", program)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	return program
}
