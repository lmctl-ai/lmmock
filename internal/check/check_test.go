package check

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaptureAndAssertions(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/first" {
			_, _ = w.Write([]byte(`{"id":"test-secret","values":[3]}`))
			return
		}
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		if b["id"] != "test-secret" {
			t.Error("capture was not passed")
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer s.Close()
	suite := Suite{Steps: []Step{{Path: "/first", JSON: map[string]any{"values.0": float64(3)}, Capture: map[string]string{"id": "id"}}, {Method: "POST", Path: "/second", Body: map[string]any{"id": "${id}"}, JSON: map[string]any{"ok": true}}}}
	n, e := Run(context.Background(), s.URL, suite)
	if e != nil || n != 2 {
		t.Fatal(n, e)
	}
	suite.Steps[0].JSON = map[string]any{"missing": "test-secret"}
	_, e = Run(context.Background(), s.URL, suite)
	if e == nil {
		t.Fatal("missing assertion accepted")
	}
}
func TestRefusals(t *testing.T) {
	for _, base := range []string{"", "://bad", "file:///tmp/test"} {
		if _, e := Run(context.Background(), base, Suite{}); e == nil {
			t.Fatal("bad URL accepted")
		}
	}
	if _, e := Run(context.Background(), "http://127.0.0.1", Suite{}); e == nil {
		t.Fatal("empty suite accepted")
	}
}
