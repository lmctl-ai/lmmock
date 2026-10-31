package rpcstdio

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func jsonLinesEngine(t *testing.T, input string) (*Engine, *bytes.Buffer) {
	t.Helper()
	code, err := os.ReadFile("../../tests/streamjson/handler.dslb")
	if err != nil {
		t.Fatal(err)
	}
	audit := filepath.Join(t.TempDir(), "audit")
	if err = os.WriteFile(audit, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]any{"audit": audit, "init": map[string]any{"events": []any{map[string]any{"type": "init"}}}, "turns": []any{map[string]any{"expect": "ping", "events": []any{map[string]any{"type": "answer", "text": "pong"}}, "close": true}}})
	out := &bytes.Buffer{}
	engine, err := New(code, Options{Config: cfg}, NewWire(strings.NewReader(input), out, "line"))
	if err != nil {
		t.Fatal(err)
	}
	if err = engine.Start(map[string]any{"argv": []string{"--input-format", "stream-json"}}); err != nil {
		t.Fatal(err)
	}
	return engine, out
}
func TestJSONLinesAnnotatedDispatchAndClose(t *testing.T) {
	engine, out := jsonLinesEngine(t, "{\"type\":\"user\",\"message\":{\"role\":\"user\",\"content\":\"ping\"}}\nnot read after explicit close\n")
	if err := engine.RunJSONLines(); err != nil {
		t.Fatal(err)
	}
	if out.String() != "{\"type\":\"init\"}\n{\"text\":\"pong\",\"type\":\"answer\"}\n" {
		t.Fatalf("handler output order/closure differs: %s", out)
	}
}
func TestJSONLinesMalformedInputIsNotRepaired(t *testing.T) {
	engine, out := jsonLinesEngine(t, "{broken}\n")
	if err := engine.RunJSONLines(); err == nil {
		t.Fatal("malformed JSON input accepted")
	}
	if out.String() != "{\"type\":\"init\"}\n" {
		t.Fatalf("unexpected response after malformed input: %s", out)
	}
}
