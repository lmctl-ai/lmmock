// Package testutil materializes trusted test programs with the same published
// compiler used by release CI. This is fixture tooling, not a runtime dependency.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func Compile(t testing.TB, kind, body string) []byte {
	t.Helper()
	dir := t.TempDir()
	schema, err := wire.Schema(kind)
	if err != nil {
		t.Fatal(err)
	}
	schemaFile := filepath.Join(dir, "schema.json")
	if err = os.WriteFile(schemaFile, schema, 0600); err != nil {
		t.Fatal(err)
	}
	q, _ := json.Marshal(filepath.ToSlash(schemaFile))
	source := "program = _lmcode_ schema=" + string(q) + ";\n" + body + "\nend _lmcode_\n"
	input, output := filepath.Join(dir, "program.lms"), filepath.Join(dir, "program.lmcode")
	if err = os.WriteFile(input, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	_, ownFile, _, _ := runtime.Caller(0)
	adapter := filepath.Join(filepath.Dir(ownFile), "..", "..", "..", "scripts", "capture-fixture.mjs")
	command := exec.Command("node", adapter, input, output)
	if b, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fixture compile: %v\n%s", err, b)
	}
	b, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func Start(t testing.TB, e *wire.Engine) (address string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(fmt.Errorf("server shutdown: %w", err))
		}
	})
	return ln.Addr().String()
}
