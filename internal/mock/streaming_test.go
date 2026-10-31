package mock

import (
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

type phaseWriter struct {
	*httptest.ResponseRecorder
	state  *store
	phases []string
}

func (w *phaseWriter) Flush() {
	w.state.Lock()
	w.phases = append(w.phases, w.state.values["stream-stage"])
	w.state.Unlock()
	w.ResponseRecorder.Flush()
}
func TestStreamingTokensBeforeHandlerAdvances(t *testing.T) {
	e, err := New(fixture(t, "streaming"))
	if err != nil {
		t.Fatal(err)
	}
	w := &phaseWriter{ResponseRecorder: httptest.NewRecorder(), state: e.state}
	e.ServeHTTP(w, httptest.NewRequest("GET", "/tokens", nil))
	if !reflect.DeepEqual(w.phases, []string{"one", "two"}) {
		t.Fatalf("tokens buffered until handler completion: flush phases %v", w.phases)
	}
	if w.Body.String() != "data: one\n\ndata: two\n\n" {
		t.Fatal("token order changed")
	}
}

func TestStreamingFramingFaults(t *testing.T) {
	engine, err := New(fixture(t, "streaming"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(engine)
	defer server.Close()
	for _, kind := range []string{"disconnect", "truncated-chunk", "malformed-chunk"} {
		t.Run(kind, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + "/fault/" + kind)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(response.Body)
			response.Body.Close()
			if response.StatusCode != 200 || err == nil || !strings.HasPrefix(string(body), "data: prefix\n\n") {
				t.Fatalf("fault was buffered or repaired: status=%d body=%q err=%v", response.StatusCode, body, err)
			}
			if strings.Contains(string(body), "handler error") {
				t.Fatal("appended HTTP 500 to committed stream")
			}
		})
	}
	response, err := server.Client().Get(server.URL + "/committed-error")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != 200 || string(body) != "prefix" || err == nil {
		t.Fatalf("committed failure rewritten: %q %v", body, err)
	}
	response, err = server.Client().Get(server.URL + "/bad-fault")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 500 {
		t.Fatal("unknown fault accepted")
	}
}
