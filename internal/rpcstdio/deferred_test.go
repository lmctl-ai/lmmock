package rpcstdio

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestDeferredReplyLifecycle(t *testing.T) {
	code, e := os.ReadFile("testdata/deferred.dslb")
	if e != nil {
		t.Fatal(e)
	}
	run := func(input string) (string, error) {
		var out bytes.Buffer
		engine, e := New(code, Options{Config: []byte("{}")}, NewWire(strings.NewReader(input), &out, "line"))
		if e != nil {
			return "", e
		}
		e = engine.Run()
		return out.String(), e
	}
	input := rpc("held", "wait", nil) + rpc(nil, "finish", nil)
	out, e := run(input)
	if e != nil {
		t.Fatal(out, e)
	}
	messages := decodeLines(t, out)
	if len(messages) != 2 || messages[0]["method"] != "ready" || messages[1]["id"] != "held" || messages[1]["result"].(map[string]any)["done"] != true {
		t.Fatal(out)
	}
	for _, input := range []string{rpc("held", "wait", nil), rpc(nil, "finish", nil), input + rpc(nil, "finish", nil), rpc("held", "wait", nil) + rpc("held", "wait", nil), rpc(nil, "wait", nil)} {
		if _, e = run(input); e == nil {
			t.Fatal("invalid deferred lifecycle accepted")
		}
	}
}
