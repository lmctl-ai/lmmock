package mock

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestRequestLogAndResetFromLMCode(t *testing.T) {
	for _, bodies := range []bool{false, true} {
		e, err := NewWithOptions(fixture(t, "streaming"), Options{RequestLog: true, RequestLogBodies: bodies})
		if err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(e)
		request := httptest.NewRequest("GET", "/tokens?token=synthetic-query-secret", strings.NewReader(`{"password":"synthetic-body-secret","echo":"synthetic-header-secret"}`))
		request.Header.Set("Authorization", "Bearer synthetic-header-secret")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, request)
		for _, path := range []string{"/missing", "/bad-fault", "/fault/truncated-chunk"} {
			r, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatal(err)
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
		}
		r, err := server.Client().Get(server.URL + "/__requests")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"synthetic-header-secret", "synthetic-query-secret", "synthetic-body-secret"} {
			if strings.Contains(string(raw), secret) {
				t.Fatal("request log leaked a synthetic credential")
			}
		}
		var log struct {
			Enabled  bool            `json:"enabled"`
			Requests []requestRecord `json:"requests"`
		}
		if err = json.Unmarshal(raw, &log); err != nil {
			t.Fatal(err)
		}
		if !log.Enabled || len(log.Requests) != 4 {
			t.Fatal("request log missing mocked/unmatched/fault requests")
		}
		if (log.Requests[0].Body != nil) != bodies {
			t.Fatal("body logging not explicitly controlled")
		}
		if log.Requests[1].Status != 404 || log.Requests[2].Status != 500 || log.Requests[3].Status != 200 || log.Requests[3].Error != "truncated-chunk" {
			t.Fatal("request log lost status/fault association")
		}
		e.state.Lock()
		e.state.counts["test"] = 7
		e.state.Unlock()
		reset, err := server.Client().Post(server.URL+"/__reset", "application/json", nil)
		if err != nil {
			t.Fatal(err)
		}
		reset.Body.Close()
		e.state.Lock()
		left := len(e.state.values) + len(e.state.counts)
		e.state.Unlock()
		if left != 0 {
			t.Fatal("reset retained shared state/counters")
		}
		r, err = server.Client().Get(server.URL + "/__requests")
		if err != nil {
			t.Fatal(err)
		}
		raw, _ = io.ReadAll(r.Body)
		r.Body.Close()
		if string(raw) != `{"enabled":true,"requests":[]}` {
			t.Fatal("reset retained history or logged admin calls")
		}
		server.Close()
	}
}
func TestScenarioResetRewindsOccurrences(t *testing.T) {
	s := `{"format":"lmmock-scenario/1","rules":[{"id":"a","group":"same","nth":1,"when":{"method":"POST","path":"/retry","body_b64":"","auth":"","auth_header":""},"reply":{"status":429,"headers":{},"body_b64":""}},{"id":"b","group":"same","nth":2,"when":{"method":"POST","path":"/retry","body_b64":"","auth":"","auth_header":""},"reply":{"status":200,"headers":{},"body_b64":""}}]}`
	file := t.TempDir() + "/config.json"
	if err := os.WriteFile(file, []byte(s), 0600); err != nil {
		t.Fatal(err)
	}
	e, err := NewWithOptions(fixture(t, "scenario"), Options{ConfigFile: file})
	if err != nil {
		t.Fatal(err)
	}
	for _, code := range []int{429, 200, 501} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("POST", "/retry", nil))
		if w.Code != code {
			t.Fatal("occurrence sequence", w.Code)
		}
	}
	e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/__reset", nil))
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/retry", nil))
	if w.Code != 429 {
		t.Fatal("reset did not rewind scenario occurrence")
	}
}
