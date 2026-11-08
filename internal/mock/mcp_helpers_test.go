package mock

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/url"
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
