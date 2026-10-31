package rpcstdio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
)

func configTest(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("../../tests/stdio/scenario.json")
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func rpc(id any, method string, params any) string {
	m := map[string]any{"jsonrpc": "2.0", "method": method, "params": params}
	if id != nil {
		m["id"] = id
	}
	b, _ := json.Marshal(m)
	return string(b) + "\n"
}
func runTest(t *testing.T, preset, alias, input, state string) (string, error) {
	t.Helper()
	code, e := Preset(preset)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	engine, e := New(code, Options{Config: configTest(t), Alias: alias, StateDir: state}, NewWire(strings.NewReader(input), &out, "line"))
	if e != nil {
		return out.String(), e
	}
	if e = engine.Start(map[string]any{"argv": []string{"-p", "ping"}}); e != nil {
		return out.String(), e
	}
	if preset != "claude" && preset != "gemini" && preset != "agy" {
		e = engine.Run()
	}
	return out.String(), e
}
func openACP() string {
	return rpc("init", "initialize", map[string]any{"protocolVersion": 1}) + rpc(nil, "notifications/initialized", nil) + rpc("open", "session/new", map[string]any{})
}
func prompt() string {
	return rpc("p", "session/prompt", map[string]any{"sessionId": "fixture-session", "prompt": []any{map[string]any{"type": "text", "text": "ping"}}})
}
func decodeLines(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	d := json.NewDecoder(strings.NewReader(s))
	for {
		var m map[string]any
		e := d.Decode(&m)
		if e == io.EOF {
			return out
		}
		if e != nil {
			t.Fatal(e, s)
		}
		out = append(out, m)
	}
}
func TestACPStreamingAndID(t *testing.T) {
	out, e := runTest(t, "acp", "Worker", openACP()+prompt(), "")
	if e != nil {
		t.Fatal(e)
	}
	m := decodeLines(t, out)
	if len(m) != 5 || m[0]["id"] != "init" || m[1]["id"] != "open" || m[4]["id"] != "p" {
		t.Fatal(out)
	}
	for i, want := range []string{"po", "ng"} {
		p := m[i+2]["params"].(map[string]any)
		if m[i+2]["method"] != "session/update" || p["sessionId"] != "fixture-session" || p["update"].(map[string]any)["content"].(map[string]any)["text"] != want {
			t.Fatal(out)
		}
	}
	if m[4]["result"].(map[string]any)["stopReason"] != "end_turn" {
		t.Fatal(out)
	}
}
func TestACPResumeConfigureAndFailures(t *testing.T) {
	init := rpc(1, "initialize", map[string]any{"protocolVersion": 1}) + rpc(nil, "notifications/initialized", nil)
	load := rpc(2, "session/load", map[string]any{"sessionId": "fixture-session"})
	steps := load
	for _, method := range []string{"session/set_mode", "session/set_model", "session/set_config_option"} {
		steps += rpc(method, method, map[string]any{"sessionId": "fixture-session", "value": "auto", "configId": "mode", "modeId": "dont_ask", "modelId": "fixture-model"})
	}
	if _, e := runTest(t, "acp", "Worker", init+steps+prompt(), ""); e != nil {
		t.Fatal(e)
	}
	for _, input := range []string{prompt(), rpc(1, "initialize", map[string]any{"protocolVersion": 2}), init + rpc(2, "session/load", map[string]any{"sessionId": "wrong"}), openACP() + prompt() + prompt() + prompt(), rpc(1, "not/scripted", nil)} {
		if _, e := runTest(t, "acp", "Worker", input, ""); e == nil {
			t.Fatal("unscripted/invalid exchange accepted")
		}
	}
	out, e := runTest(t, "acp", "Error", openACP()+prompt(), "")
	if e == nil || !strings.Contains(out, "fixture failure") {
		t.Fatal(out, e)
	}
	out, e = runTest(t, "acp", "Empty", openACP()+prompt(), "")
	if e != nil || !strings.Contains(out, `"text":""`) {
		t.Fatal(out, e)
	}
}
func TestAgentCellsPersistAndIsolate(t *testing.T) {
	for _, preset := range []string{"claude", "gemini", "agy"} {
		t.Run(preset, func(t *testing.T) {
			state := t.TempDir()
			a, e := runTest(t, preset, "Worker", "", state)
			if e != nil || !strings.Contains(a, "pong") {
				t.Fatal(a, e)
			}
			b, e := runTest(t, preset, "Worker", "", state)
			if e != nil || !strings.Contains(b, "second") {
				t.Fatal(b, e)
			}
			if _, e = runTest(t, preset, "Worker", "", state); e == nil {
				t.Fatal("unscripted turn succeeded")
			}
			peer, e := runTest(t, preset, "Peer", "", state)
			if e != nil || !strings.Contains(peer, "peer") {
				t.Fatal("alias counter shared", peer, e)
			}
			if _, e = runTest(t, preset, "Other", "", state); e == nil {
				t.Fatal("alias leaked")
			}
			if _, e = runTest(t, preset, "Error", "", state); e == nil {
				t.Fatal("scripted error ignored")
			}
		})
	}
}
func TestCodexTurnOrder(t *testing.T) {
	input := rpc(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "lmctl", "version": "0.2.448"}}) + rpc(nil, "initialized", nil) + rpc(2, "thread/start", map[string]any{}) + rpc(3, "turn/start", map[string]any{"threadId": "fixture-session", "input": "ping"})
	out, e := runTest(t, "codex", "Worker", input, "")
	if e != nil {
		t.Fatal(e)
	}
	m := decodeLines(t, out)
	if len(m) != 8 || m[2]["id"] != float64(3) || m[3]["method"] != "turn/started" || m[5]["method"] != "item/agentMessage/delta" || m[7]["method"] != "turn/completed" {
		t.Fatal(out)
	}
}
func TestMCPToolsResourcesPrompts(t *testing.T) {
	in := rpc(1, "initialize", map[string]any{"protocolVersion": "2025-11-25"}) + rpc(nil, "notifications/initialized", nil)
	for i, m := range []string{"tools/list", "resources/list", "resources/templates/list", "prompts/list", "ping"} {
		in += rpc(10+i, m, map[string]any{})
	}
	in += rpc(20, "resources/read", map[string]any{"uri": "fixture:///note"}) + rpc(21, "prompts/get", map[string]any{"name": "fixture"}) + rpc(22, "tools/call", map[string]any{"name": "fixture", "arguments": map[string]any{}})
	out, e := runTest(t, "mcp", "MCP", in, "")
	if e != nil {
		t.Fatal(e)
	}
	for _, part := range []string{`"tools":`, `"resources":`, `"resourceTemplates":`, `"prompts":`, `fixture note`, `fixture prompt`, `notifications/message`, `pong`} {
		if !strings.Contains(out, part) {
			t.Fatal("missing", part, out)
		}
	}
	if _, e = runTest(t, "mcp", "MCP", in+rpc(23, "tools/call", map[string]any{"name": "fixture"}), ""); e == nil {
		t.Fatal("unscripted MCP call")
	}
}
func TestMCPClientCorrelation(t *testing.T) {
	input := ""
	for i, res := range []string{`{"protocolVersion":"2025-11-25"}`, `{"tools":[{"name":"fixture"}]}`, `{"content":[{"type":"text","text":"pong"}]}`} {
		input += fmt.Sprintf("{\"jsonrpc\":\"2.0\",\"id\":%d,\"result\":%s}\n", i+1, res)
	}
	out, e := runTest(t, "mcp-client", "MCP", input, "")
	if e != nil || !strings.Contains(out, `"method":"tools/call"`) {
		t.Fatal(out, e)
	}
	bad, e := runTest(t, "mcp-client", "MCP", strings.Replace(input, `"id":2`, `"id":99`, 1), "")
	if e == nil || strings.Contains(bad, `"method":"tools/call"`) {
		t.Fatal("wrong response id dispatched next step", bad, e)
	}
	if _, e = runTest(t, "mcp-client", "MCP", strings.Replace(input, "pong", "wrong", 1), ""); e == nil {
		t.Fatal("wrong tool result accepted")
	}
	if _, e = runTest(t, "mcp-client", "MCP", "", ""); e == nil {
		t.Fatal("EOF with pending request accepted")
	}
}
func TestMalformedRPC(t *testing.T) {
	for _, in := range []string{"{bad}\n", "[]\n", `{"jsonrpc":"1.0","id":1,"method":"initialize"}` + "\n", `{"jsonrpc":"2.0","id":true,"method":"initialize"}` + "\n", `{"id":1,"result":{}}` + "\n"} {
		if _, e := runTest(t, "acp", "Worker", in, ""); e == nil {
			t.Fatal("invalid message accepted", in)
		}
	}
}

type flushWriter struct {
	bytes.Buffer
	flushes int
	fail    bool
}

func (w *flushWriter) Flush() error {
	w.flushes++
	if w.fail {
		return io.ErrClosedPipe
	}
	return nil
}
func TestWireFlushFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		w := &flushWriter{fail: fail}
		err := NewWire(strings.NewReader(""), w, "line").Write([]byte(`{}`))
		if (err != nil) != fail || w.flushes != 1 {
			t.Fatal(err, w.flushes)
		}
	}
}
func TestACPContentLength(t *testing.T) {
	var input bytes.Buffer
	for _, line := range strings.Split(strings.TrimSpace(openACP()+prompt()), "\n") {
		if e := NewWire(strings.NewReader(""), &input, "content-length").Write([]byte(line)); e != nil {
			t.Fatal(e)
		}
	}
	var output bytes.Buffer
	code, _ := Preset("acp")
	e, err := New(code, Options{Alias: "Worker", Config: configTest(t)}, NewWire(&input, &output, "content-length"))
	if err != nil {
		t.Fatal(err)
	}
	if err = e.Run(); err != nil {
		t.Fatal(err)
	}
	r := NewWire(&output, io.Discard, "content-length")
	count := 0
	for {
		_, err = r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		count++
	}
	if count != 5 {
		t.Fatal(count)
	}
}
func TestRegistryAndConfigRefusals(t *testing.T) {
	code, _ := Preset("acp")
	for _, b := range [][]byte{[]byte("bad"), append([]byte{}, code[:len(code)-1]...)} {
		if _, e := New(b, Options{Config: configTest(t)}, NewWire(strings.NewReader(""), io.Discard, "line")); e == nil {
			t.Fatal("bad program accepted")
		}
	}
	if _, e := New(code, Options{Config: []byte("bad")}, NewWire(strings.NewReader(""), io.Discard, "line")); e == nil {
		t.Fatal("bad config accepted")
	}
}
