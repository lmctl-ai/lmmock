package rpcstdio

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func codexCase(t *testing.T, selected, input string) ([]map[string]any, error) {
	t.Helper()
	raw, e := os.ReadFile("../../fixtures/codex/wire.json")
	if e != nil {
		t.Fatal(e)
	}
	var cfg map[string]any
	if e = json.Unmarshal(raw, &cfg); e != nil {
		t.Fatal(e)
	}
	cfg["selected"] = selected
	cfg["cells"] = cfg["cases"].(map[string]any)[selected].(map[string]any)["cells"]
	raw, _ = json.Marshal(cfg)
	code, e := Preset("codex")
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	engine, e := New(code, Options{Alias: "Worker", Config: raw}, NewWire(strings.NewReader(input), &out, "line"))
	if e != nil {
		t.Fatal(e)
	}
	e = engine.Run()
	return decodeLines(t, out.String()), e
}
func codexHello() string {
	return rpc(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "lmctl", "version": "0.2.448"}, "capabilities": map[string]any{"experimentalApi": false}}) + rpc(nil, "initialized", map[string]any{})
}

const codexFixtureID = "11111111-1111-4111-8111-111111111111"

func TestCodexProviderUsageLifecycle(t *testing.T) {
	input := codexHello() + rpc(2, "thread/start", map[string]any{"model": "gpt-5.6-sol"}) + rpc(3, "turn/start", map[string]any{"threadId": codexFixtureID, "input": []any{map[string]any{"type": "text", "text": "ping"}}})
	out, e := codexCase(t, "success", input)
	if e != nil {
		t.Fatal(e)
	}
	methods := []string{}
	for _, m := range out {
		if method, ok := m["method"].(string); ok {
			methods = append(methods, method)
		}
	}
	want := "turn/started,item/started,item/agentMessage/delta,item/agentMessage/delta,item/completed,thread/tokenUsage/updated,turn/completed"
	if strings.Join(methods, ",") != want {
		t.Fatalf("Codex lifecycle/usage event order: %v", methods)
	}
	u := out[len(out)-2]["params"].(map[string]any)["tokenUsage"].(map[string]any)["last"].(map[string]any)
	if u["inputTokens"] != float64(100) || u["cachedInputTokens"] != float64(30) || u["reasoningOutputTokens"] != float64(4) {
		t.Fatalf("Codex exact token facts: %v", u)
	}
}
func TestCodexProviderResumeFork(t *testing.T) {
	input := codexHello() + rpc(2, "thread/resume", map[string]any{"threadId": codexFixtureID}) + rpc(3, "thread/fork", map[string]any{"threadId": codexFixtureID})
	out, e := codexCase(t, "fork", input)
	if e != nil {
		t.Fatalf("Codex resume/fork: %v", e)
	}
	thread := out[len(out)-1]["result"].(map[string]any)["thread"].(map[string]any)
	if thread["id"] != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("Codex fork identity: %v", thread)
	}
}
func TestCodexProviderRequiresHandshake(t *testing.T) {
	out, e := codexCase(t, "success", rpc(1, "thread/start", map[string]any{}))
	if e != nil {
		t.Fatal(e)
	}
	if len(out) != 1 || out[0]["error"] == nil {
		t.Fatalf("Codex initialize ordering must refuse open: %v", out)
	}
}
func TestCodexProviderInterrupt(t *testing.T) {
	input := codexHello() + rpc(2, "thread/resume", map[string]any{"threadId": codexFixtureID}) + rpc(3, "turn/start", map[string]any{"threadId": codexFixtureID, "input": "cancel"}) + rpc(4, "turn/interrupt", map[string]any{"threadId": codexFixtureID, "turnId": "turn-1"})
	out, e := codexCase(t, "interrupt", input)
	if e != nil {
		t.Fatalf("Codex interrupt: %v", e)
	}
	last := out[len(out)-1]
	turn := last["params"].(map[string]any)["turn"].(map[string]any)
	if last["method"] != "turn/completed" || turn["status"] != "interrupted" || turn["id"] != "turn-1" {
		t.Fatalf("Codex interrupt terminal: %v", last)
	}
}

func TestCodexProviderAdverseFidelity(t *testing.T) {
	input := codexHello() + rpc(2, "thread/start", map[string]any{}) + rpc(3, "turn/start", map[string]any{"threadId": codexFixtureID, "input": "ping"})
	for _, selected := range []string{"malformed-usage", "wrong-turn-id", "rpc-error"} {
		t.Run(selected, func(t *testing.T) {
			out, err := codexCase(t, selected, input)
			if err != nil {
				t.Fatal(err)
			}
			switch selected {
			case "malformed-usage":
				var found any
				for _, m := range out {
					if m["method"] == "thread/tokenUsage/updated" {
						found = m["params"].(map[string]any)["tokenUsage"].(map[string]any)["last"].(map[string]any)["inputTokens"]
					}
				}
				if found != "not-an-integer" {
					t.Fatalf("malformed usage must not be repaired: %v", found)
				}
			case "wrong-turn-id":
				last := out[len(out)-1]
				if last["params"].(map[string]any)["turn"].(map[string]any)["id"] != "wrong-turn" {
					t.Fatalf("wrong-ID terminal must not be repaired: %v", last)
				}
			case "rpc-error":
				last := out[len(out)-1]
				if last["error"].(map[string]any)["code"] != float64(-32001) {
					t.Fatalf("Codex RPC error must retain code: %v", last)
				}
			}
		})
	}
}

func TestCodexProviderLegacyCellsHaveDistinctTurnIDs(t *testing.T) {
	input := rpc(1, "initialize", map[string]any{"clientInfo": map[string]any{"name": "lmctl", "version": "0.0.0"}}) + rpc(nil, "initialized", nil) + rpc(2, "thread/resume", map[string]any{"threadId": "fixture-session"}) + rpc(3, "turn/start", map[string]any{"threadId": "fixture-session", "input": "ping"}) + rpc(4, "turn/start", map[string]any{"threadId": "fixture-session", "input": "ping"})
	out, err := runTest(t, "codex", "Worker", input, "")
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, m := range decodeLines(t, out) {
		if m["method"] == "turn/completed" {
			ids = append(ids, m["params"].(map[string]any)["turn"].(map[string]any)["id"].(string))
		}
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("consecutive legacy cells need distinct turn IDs: %v", ids)
	}
}

func TestCodexProviderMethodSAMEntries(t *testing.T) {
	raw, err := os.ReadFile("../../fixtures/providers/codex-app-server.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Routes []struct {
			Annotation string `json:"annotation"`
			SAM        string `json:"sam"`
		} `json:"routes"`
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	annotations := map[string]bool{}
	for _, route := range manifest.Routes {
		if route.SAM != "RPCHandler" || annotations[route.Annotation] {
			t.Fatalf("each Codex method requires its own declared RPCHandler entry: %s", route.Annotation)
		}
		annotations[route.Annotation] = true
	}
}
