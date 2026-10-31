package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScenarioCLIAtomicRefusal(t *testing.T) {
	d := t.TempDir()
	input := filepath.Join(d, "raw")
	out := filepath.Join(d, "out")
	os.WriteFile(input, []byte(`{"error":"no response"}`), 0600)
	os.WriteFile(out, []byte("prior"), 0600)
	if scenarioCommand([]string{"import", "--input", input, "--output", out}) == 0 {
		t.Fatal("bad capture accepted")
	}
	b, _ := os.ReadFile(out)
	if string(b) != "prior" {
		t.Fatal("failed conversion replaced output")
	}
	os.WriteFile(input, []byte(`{"request":{"method":"GET","path":"/","headers":{},"content":""},"response":{"status_code":200,"headers":{},"content":""}}`), 0600)
	if scenarioCommand([]string{"import", "--input", input, "--output", out}) != 0 {
		t.Fatal("valid capture failed")
	}
	b, _ = os.ReadFile(out)
	if string(b) == "prior" {
		t.Fatal("output not replaced")
	}
	if scenarioCommand([]string{"verify", "--input", input, "--target", "https://example.invalid"}) == 0 {
		t.Fatal("vendor target allowed")
	}
	if scenarioCommand([]string{"import", "--input", input, "--output", out, "--target", "http://127.0.0.1"}) != 2 {
		t.Fatal("unused option accepted")
	}
}
