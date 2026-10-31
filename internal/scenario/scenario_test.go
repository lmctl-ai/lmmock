package scenario

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
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
