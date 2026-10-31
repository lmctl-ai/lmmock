package scenario

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestScrubKnownCredentials(t *testing.T) {
	token := "fake-seeded-token-DO-NOT-EXPORT"
	f := Flow{Request: Request{Method: "POST", Path: "/x?key=" + token, Headers: map[string][]string{"Authorization": {"Bearer " + token}}, Content: []byte(`{"echo":"` + token + `"}`)}, Response: &Response{Status: 200, Headers: map[string][]string{"Content-Type": {"text/plain"}, "Set-Cookie": {token}}, Content: []byte(token)}}
	raw, _ := json.Marshal(f)
	s, clean, e := Import(bytes.NewReader(raw), false)
	if e != nil {
		t.Fatal(e)
	}
	a, _ := json.Marshal(s)
	b, _ := json.Marshal(clean)
	for _, out := range []string{string(a), string(b), string(clean[0].Response.Content), string(clean[0].Request.Content)} {
		if strings.Contains(out, token) {
			t.Fatal("seeded token leaked")
		}
	}
}
func TestSSEOrder(t *testing.T) {
	events := Events("event: a\r\ndata: 1\r\n\r\nevent: b\ndata: 2\n\nlast")
	if strings.Join(events, "") != "event: a\r\ndata: 1\r\n\r\nevent: b\ndata: 2\n\nlast" || len(events) != 3 {
		t.Fatal(events)
	}
}

func TestGzipBinaryErrorsAndNormalization(t *testing.T) {
	for _, body := range [][]byte{[]byte("a\x00\xff\r\n"), []byte(`{"id":"resp_12345678","created":9999999999,"n":9007199254740993}`)} {
		var zipped bytes.Buffer
		w := gzip.NewWriter(&zipped)
		w.Write(body)
		w.Close()
		f := Flow{Request: Request{Method: "GET", Path: "/x?a=1"}, Response: &Response{Status: 429, Headers: map[string][]string{"Content-Encoding": {"gzip"}, "Retry-After": {"1"}}, Content: zipped.Bytes()}}
		raw, _ := json.Marshal(f)
		s, clean, e := Import(bytes.NewReader(raw), false)
		if e != nil {
			t.Fatal(e)
		}
		if s.Rules[0].Reply.Status != 429 || s.Rules[0].Reply.Headers["retry-after"] != "1" {
			t.Fatal(s)
		}
		if bytes.Contains(body, []byte("\xff")) && !bytes.Equal(body, clean[0].Response.Content) {
			t.Fatal("binary changed")
		}
		if bytes.Contains(body, []byte("9007199254740993")) && !bytes.Contains(clean[0].Response.Content, []byte("9007199254740993")) {
			t.Fatal("integer rounded")
		}
	}
	for _, raw := range []string{"", `{"request":{},"error":"upstream down"}`, `{"request":{},"response":{"status_code":999}}`} {
		if _, _, e := Import(strings.NewReader(raw), false); e == nil {
			t.Fatal("accepted malformed/incomplete flow")
		}
	}
}
func TestEscapedSSESecretAndFutureCredential(t *testing.T) {
	token := "seeded-secret-token"
	escaped := strings.ReplaceAll(token, "s", `\u0073`)
	first := Flow{Request: Request{Method: "GET", Path: "/x"}, Response: &Response{Status: 200, Headers: map[string][]string{"Content-Type": {"text/event-stream"}}, Content: []byte("data: {\"echo\":\"" + escaped + "\"}\n\n")}}
	second := first
	second.Request.Headers = map[string][]string{"Authorization": {"Bearer " + token}}
	var raw bytes.Buffer
	json.NewEncoder(&raw).Encode(first)
	json.NewEncoder(&raw).Encode(second)
	_, flows, e := Import(&raw, false)
	if e != nil {
		t.Fatal(e)
	}
	var data map[string]string
	payload := strings.TrimSpace(strings.TrimPrefix(string(flows[0].Response.Content), "data: "))
	if json.Unmarshal([]byte(payload), &data) != nil {
		t.Fatal(payload)
	}
	if data["echo"] == token {
		t.Fatal("escaped SSE secret leaked")
	}
}

func TestCopilotCaseStudyContract(t *testing.T) {
	file, e := os.Open("../../captures/copilot/flows.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	defer file.Close()
	_, flows, e := Import(file, false)
	if e != nil {
		t.Fatal(e)
	}
	seen := map[string]bool{}
	for _, f := range flows {
		if f.Response.Status != 200 {
			if f.Request.Path != "/models" || f.Response.Status != 401 {
				t.Fatal("unexpected failure")
			}
			continue
		}
		seen[f.Request.Path] = true
		switch f.Request.Path {
		case "/models":
			var catalog struct {
				Data []struct {
					ID           string
					Default      bool `json:"is_chat_default"`
					Fallback     bool `json:"is_chat_fallback"`
					Picker       bool `json:"model_picker_enabled"`
					Capabilities struct {
						Type, Family string
						Limits       map[string]int
					}
					Endpoints []string `json:"supported_endpoints"`
				}
			}
			if e = json.Unmarshal(f.Response.Content, &catalog); e != nil {
				t.Fatal(e)
			}
			models := map[string]bool{}
			for _, m := range catalog.Data {
				models[m.ID] = true
				if m.ID == "gpt-5.3-codex" && (!m.Default || !m.Fallback || !m.Picker || m.Capabilities.Type != "chat" || m.Capabilities.Family != "gpt-5.3-codex" || m.Capabilities.Limits["max_prompt_tokens"] <= 0 || len(m.Endpoints) == 0) {
					t.Fatal("Copilot startup catalog incomplete")
				}
			}
			for _, id := range []string{"gpt-5.3-codex", "gpt-4o", "claude-haiku-4.5"} {
				if !models[id] {
					t.Fatal("missing model", id)
				}
			}
		case "/auto":
			var selected struct {
				Model struct{ ID string } `json:"selected_model"`
			}
			if json.Unmarshal(f.Response.Content, &selected) != nil || selected.Model.ID == "" {
				t.Fatal("missing selected_model")
			}
		case "/responses", "/v1/responses", "/v1/messages":
			names := []string{"response.created", "response.in_progress", "response.output_item.added", "response.content_part.added", "response.output_text.delta", "response.output_text.done", "response.content_part.done", "response.output_item.done", "response.completed"}
			responses := f.Request.Path != "/v1/messages"
			if !responses {
				names = []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
			}
			events := Events(string(f.Response.Content))
			if len(events) != len(names) {
				t.Fatal("event count", f.Request.Path, len(events))
			}
			for i, event := range events {
				lines := strings.Split(event, "\n")
				if lines[0] != "event: "+names[i] {
					t.Fatal("event transition", i, lines[0])
				}
				var payload struct {
					Type     string
					Sequence int `json:"sequence_number"`
				}
				if json.Unmarshal([]byte(strings.TrimPrefix(lines[1], "data: ")), &payload) != nil || payload.Type != names[i] || responses && payload.Sequence != i+1 {
					t.Fatal("event payload/sequence", i)
				}
			}
		}
	}
	for _, path := range []string{"/models", "/auto", "/responses", "/v1/messages", "/copilot_internal/team_customizations", "/copilot_internal/managed_settings", "/mcp/readonly", "/telemetry"} {
		if !seen[path] {
			t.Fatal("missing handshake pattern", path)
		}
	}
}

func TestScrubQueryAndBodyCredentialsWithoutHeaders(t *testing.T) {
	for _, exact := range []bool{false, true} {
		f := Flow{Request: Request{Method: "POST", Path: "/generate?key=synthetic-query-value&access_token=synthetic-query-token", Headers: map[string][]string{"Content-Type": {"application/json"}}, Content: []byte(`{"api_key":"synthetic-body-value","nested":{"client_secret":"synthetic-client-value"}}`)}, Response: &Response{Status: 200, Content: []byte(`{"echo":"synthetic-query-value synthetic-body-value synthetic-client-value synthetic-query-token"}`)}}
		raw, _ := json.Marshal(f)
		result, clean, err := Import(bytes.NewReader(raw), exact)
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(result)
		for _, secret := range []string{"synthetic-query-value", "synthetic-query-token", "synthetic-body-value", "synthetic-client-value"} {
			for _, output := range []string{string(a), clean[0].Request.Path, string(clean[0].Request.Content), string(clean[0].Response.Content)} {
				if strings.Contains(output, secret) {
					t.Fatalf("query/body credential retained (exact=%v)", exact)
				}
			}
		}
	}
}
