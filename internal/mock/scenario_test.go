package mock

import (
	"bytes"
	"encoding/json"
	"github.com/lmctl-ai/lmmock/internal/scenario"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func scenarioEngine(t *testing.T, s scenario.Scenario) *Engine {
	t.Helper()
	raw, e := json.Marshal(s)
	if e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(t.TempDir(), "scenario.json")
	if e = os.WriteFile(file, raw, 0600); e != nil {
		t.Fatal(e)
	}
	engine, e := NewWithOptions(fixture(t, "scenario"), Options{ConfigFile: file})
	if e != nil {
		t.Fatal(e)
	}
	return engine
}
func TestScenarioFidelityAndControls(t *testing.T) {
	raw, e := os.ReadFile("../../captures/copilot/flows.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	s, _, e := scenario.Import(bytes.NewReader(raw), false)
	if e != nil {
		t.Fatal(e)
	}
	expected, e := os.ReadFile("../../captures/copilot/scenario.json")
	if e != nil {
		t.Fatal(e)
	}
	generated, e := json.MarshalIndent(s, "", "  ")
	if e != nil || !bytes.Equal(append(generated, '\n'), expected) {
		t.Fatal("committed scenario differs from scrubbed capture")
	}
	baseline := httptest.NewServer(scenarioEngine(t, s))
	defer baseline.Close()
	if e = scenario.Verify(bytes.NewReader(raw), baseline.URL); e != nil {
		t.Fatal(e)
	}
	for _, mutation := range []string{"SSE-order", "missing-rule", "changed-reply"} {
		t.Run(mutation, func(t *testing.T) {
			copy, _, e := scenario.Import(bytes.NewReader(raw), false)
			if e != nil {
				t.Fatal(e)
			}
			switch mutation {
			case "SSE-order":
				for i := range copy.Rules {
					events := copy.Rules[i].Reply.Events
					if len(events) > 1 {
						events[0], events[1] = events[1], events[0]
						break
					}
				}
			case "missing-rule":
				copy.Rules = copy.Rules[1:]
			case "changed-reply":
				copy.Rules[0].Reply.Status = 418
			}
			server := httptest.NewServer(scenarioEngine(t, copy))
			defer server.Close()
			if scenario.Verify(bytes.NewReader(raw), server.URL) == nil {
				t.Fatal("fidelity accepted " + mutation)
			}
		})
	}
}

type eventRecorder struct {
	*httptest.ResponseRecorder
	flushes int
}

func (r *eventRecorder) Flush() { r.flushes++; r.ResponseRecorder.Flush() }
func TestScenarioFlushAndOccurrences(t *testing.T) {
	var raw bytes.Buffer
	for _, status := range []int{429, 200} {
		json.NewEncoder(&raw).Encode(scenario.Flow{Request: scenario.Request{Method: "POST", Path: "/retry?model=x", Content: []byte("not-json")}, Response: &scenario.Response{Status: status, Headers: map[string][]string{"Content-Type": {"text/event-stream"}}, Content: []byte("data: one\n\ndata: two\n\n")}})
	}
	s, _, e := scenario.Import(bytes.NewReader(raw.Bytes()), false)
	if e != nil {
		t.Fatal(e)
	}
	engine := scenarioEngine(t, s)
	for _, want := range []int{429, 200, 501} {
		w := &eventRecorder{ResponseRecorder: httptest.NewRecorder()}
		engine.ServeHTTP(w, httptest.NewRequest("POST", "/retry?model=x", bytes.NewBufferString("not-json")))
		if w.Code != want {
			t.Fatal(w.Code, w.Body.String())
		}
		if want != 501 && (w.flushes != 2 || w.Body.String() != "data: one\n\ndata: two\n\n") {
			t.Fatal("stream not flushed in event order", w.flushes, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	engine.ServeHTTP(w, httptest.NewRequest("POST", "/retry?model=y", bytes.NewBufferString("not-json")))
	if w.Code != 501 {
		t.Fatal("query ignored")
	}
}
