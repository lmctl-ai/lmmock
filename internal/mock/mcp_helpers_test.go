package mock

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestPauseContextCompletionAndCancellation(t *testing.T) {
	completed, err := pauseContext(context.Background(), 0)
	if err != nil || !completed {
		t.Fatalf("zero pause = %v, %v; want complete", completed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	completed, err = pauseContext(ctx, 3600000)
	if err != nil || completed {
		t.Fatalf("cancelled pause = %v, %v; want cancelled", completed, err)
	}
	if _, err = pauseContext(context.Background(), -1); err == nil {
		t.Fatal("negative pause accepted")
	}
}

func TestMCPResponseQueuePreservesEventOrder(t *testing.T) {
	state := &store{values: map[string]string{}}
	x := &exchange{state: state, responseQueue: "session"}
	enqueueResponse(x, "Jfirst")
	enqueueResponse(x, "Esecond")
	if got := dequeueResponse(state, "session"); got != "Jfirst" {
		t.Fatalf("first queued event = %q", got)
	}
	if got := dequeueResponse(state, "session"); got != "Esecond" {
		t.Fatalf("second queued event = %q", got)
	}
	if got := dequeueResponse(state, "session"); got != "" {
		t.Fatalf("queue not empty: %q", got)
	}
}

func TestMCPResponseQueuePreservesRecordSeparatorBytes(t *testing.T) {
	state := &store{values: map[string]string{}}
	x := &exchange{state: state, responseQueue: "session"}
	withSep := "J{\"text\":\"left\x1eright\"}"
	enqueueResponse(x, withSep)
	enqueueResponse(x, "J{\"second\":true}")
	if got := dequeueResponse(state, "session"); got != withSep {
		t.Fatalf("first queued event with 0x1e = %q, want %q", got, withSep)
	}
	if got := dequeueResponse(state, "session"); got != "J{\"second\":true}" {
		t.Fatalf("second queued event = %q", got)
	}
}

func TestAtomicIncrementSyncsValueStore(t *testing.T) {
	state := &store{values: map[string]string{}, counts: map[string]int64{}}
	if got := state.increment("counter"); got != 1 {
		t.Fatalf("increment = %d; want 1", got)
	}
	if got := state.get("counter"); got != "1" {
		t.Fatalf("get after increment = %q; want \"1\"", got)
	}
}

func TestPseudoHeadersSchemeBodyKindAndMcpName(t *testing.T) {
	state := &store{values: map[string]string{}, counts: map[string]int64{}}
	req := &http.Request{
		Method: "POST",
		URL:    &url.URL{Path: "/mcp"},
		Header: http.Header{
			"Mcp-Name": []string{"=?base64?bW9jazovL2l0ZW0vaMOpbGxv?="},
		},
		TLS: &tls.ConnectionState{},
	}
	x := &exchange{request: req, body: `{"jsonrpc":"2.0"}`, state: state}

	if got := x.lookupHeader(":scheme"); got != "https" {
		t.Fatalf(":scheme on TLS = %q; want https", got)
	}
	if got := x.lookupHeader(":body_json_kind"); got != "object" {
		t.Fatalf(":body_json_kind on object = %q; want object", got)
	}
	if got := x.lookupHeader("Mcp-Name-Decoded"); got != "mock://item/héllo" {
		t.Fatalf("Mcp-Name-Decoded = %q; want mock://item/héllo", got)
	}

	xBad := &exchange{request: &http.Request{Header: http.Header{}}, body: "{bad", state: state}
	if got := xBad.lookupHeader(":body_json_kind"); got != "invalid" {
		t.Fatalf(":body_json_kind on invalid JSON = %q; want invalid", got)
	}
	if got := xBad.lookupHeader("Mcp-Name-Decoded"); got != "\x00missing" {
		t.Fatalf("Mcp-Name-Decoded when missing = %q; want \\x00missing", got)
	}
}

func TestIdempotentSetHeaderAfterStreamCommit(t *testing.T) {
	e, err := NewWithOptions(fixture(t, "mcp/server"), Options{ConfigFile: "../../tests/mcp/session-config.json"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := Start(e, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	initReq, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("MCP-Protocol-Version", "2025-11-25")
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatal(err)
	}
	sid := initResp.Header.Get("Mcp-Session-Id")
	_ = initResp.Body.Close()
	if sid == "" {
		t.Fatal("missing Mcp-Session-Id")
	}

	slowReq, _ := http.NewRequest("POST", srv.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"slow","arguments":{},"_meta":{"progressToken":"p1"}}}`))
	slowReq.Header.Set("Content-Type", "application/json")
	slowReq.Header.Set("Accept", "application/json, text/event-stream")
	slowReq.Header.Set("MCP-Protocol-Version", "2025-11-25")
	slowReq.Header.Set("Mcp-Session-Id", sid)
	slowReq.Header.Set("X-Mock-Reply-Mode", "sse")
	slowResp, err := http.DefaultClient.Do(slowReq)
	if err != nil {
		t.Fatal(err)
	}
	body, readErr := io.ReadAll(slowResp.Body)
	_ = slowResp.Body.Close()
	if readErr != nil {
		t.Fatalf("SSE slow response body truncated: err=%v body=%q", readErr, string(body))
	}
	text := string(body)
	if got := strings.Count(text, `"method":"notifications/progress"`); got != 5 {
		t.Fatalf("expected 5 notifications/progress events, got %d in %q", got, text)
	}
	if !strings.Contains(text, `"slow complete"`) {
		t.Fatalf("expected final CallToolResult in SSE stream, got %q", text)
	}
}

