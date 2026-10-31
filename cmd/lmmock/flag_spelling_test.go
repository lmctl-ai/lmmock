package main

import (
	"os"
	"path/filepath"
	"testing"
)

// Each historical route must fail at flag selection, before reading bytecode
// or creating an audit log. Keep exec's command present so unrelated usage
// validation cannot make a still-accepted option look correctly refused.
func TestRetiredHostModesRefuseBeforeInput(t *testing.T) {
	old := "--" + "dsl"
	directory := t.TempDir()
	missing := filepath.Join(directory, "must-not-open.dslb")
	audit := filepath.Join(directory, "must-not-create.jsonl")
	cases := [][]string{
		{old + "-schema", "--json"},
		{"serve", old, missing, "--audit-log", audit},
		{"exec", old, missing, "--audit-log", audit, "--", "must-not-run"},
	}
	for _, args := range cases {
		if code := run(args); code != 2 {
			t.Errorf("retired mode accepted or reached input: %v exit=%d", args, code)
		}
	}
	if _, err := os.Stat(audit); !os.IsNotExist(err) {
		t.Fatalf("retired mode created audit state: %v", err)
	}
}

func TestNewExecutionOptionReachesInput(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.dslb")
	if code := run([]string{"serve", "--lmcode", missing}); code != 1 {
		t.Fatalf("new execution option did not reach existing input-failure policy: exit=%d", code)
	}
}

func TestNewSchemaFlagReturnsExactBytes(t *testing.T) {
	output, err := os.CreateTemp(t.TempDir(), "schema-*")
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = output
	code := run([]string{"--lmcode-schema", "--json"})
	os.Stdout = previous
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("../../internal/mock/schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || string(actual) != string(expected) {
		t.Fatalf("new schema flag changed bytes or failed: exit=%d", code)
	}
}
