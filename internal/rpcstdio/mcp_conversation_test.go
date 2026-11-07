package rpcstdio

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func runMCP(t *testing.T, input string) (string, error) {
	return runMCPAs(t, "Tools", input)
}

func runMCPAs(t *testing.T, alias, input string) (string, error) {
	t.Helper()
	cfg, e := os.ReadFile("../../tests/stdio/mcp-conversation.json")
	if e != nil {
		t.Fatal(e)
	}
	code, e := Preset("mcp")
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	engine, e := New(code, Options{Config: cfg, Alias: alias}, NewWire(strings.NewReader(input), &out, "line"))
	if e != nil {
		t.Fatal(e)
	}
	if e = engine.Start(map[string]any{}); e != nil {
		return out.String(), e
	}
	e = engine.Run()
	return out.String(), e
}

func mcpOpen() string {
	return rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}) + rpc(nil, "notifications/initialized", nil)
}
func mcpCall(id int, city string) string {
	return rpc(id, "tools/call", map[string]any{"name": "fixture", "arguments": map[string]any{"city": city}})
}

// One initialized session serves several tools/call requests; each call consumes
// the next scripted cell and emits a log notification before its result.
func TestMCPMultiCallSession(t *testing.T) {
	out, e := runMCP(t, mcpOpen()+rpc(2, "tools/list", map[string]any{})+mcpCall(3, "berlin")+mcpCall(4, "tokyo"))
	if e != nil {
		t.Fatal(e, out)
	}
	for _, part := range []string{`"name":"fixture"`, "Berlin: 12C, cloudy", "Tokyo: 21C, clear"} {
		if !strings.Contains(out, part) {
			t.Fatal("missing", part, out)
		}
	}
	if strings.Count(out, "notifications/message") != 2 {
		t.Fatal("expected one log notification per call", out)
	}
	if strings.Index(out, "Berlin") > strings.Index(out, "Tokyo") {
		t.Fatal("results out of order", out)
	}
}

func TestMCPScriptedToolError(t *testing.T) {
	out, e := runMCP(t, mcpOpen()+mcpCall(2, "berlin")+mcpCall(3, "tokyo")+mcpCall(4, "paris")+mcpCall(5, "oslo"))
	if e == nil {
		t.Fatal("scripted error cell did not stop the server")
	}
	msgs := decodeLines(t, out)
	last := msgs[len(msgs)-1]
	errObj, _ := last["error"].(map[string]any)
	if last["id"] != float64(5) || errObj == nil || errObj["message"] != "no data for oslo" {
		t.Fatal(out)
	}
}

func TestMCPCallRequiresInitializedNotification(t *testing.T) {
	in := rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}) + mcpCall(2, "berlin")
	if _, e := runMCP(t, in); e == nil {
		t.Fatal("tools/call served before notifications/initialized")
	}
}

func TestMCPExpectationAndExhaustion(t *testing.T) {
	if _, e := runMCP(t, mcpOpen()+mcpCall(2, "madrid")); e == nil {
		t.Fatal("call that violates the cell expectation was answered")
	}
	if _, e := runMCPAs(t, "Short", mcpOpen()+mcpCall(2, "x")); e != nil {
		t.Fatal(e)
	}
	if _, e := runMCPAs(t, "Short", mcpOpen()+mcpCall(2, "x")+mcpCall(3, "y")); e == nil {
		t.Fatal("second call answered although one cell is scripted")
	}
}
