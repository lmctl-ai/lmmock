package mock

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "build", name+".dslb"))
	if err != nil {
		t.Fatal("run lmscript test.lms to compile fixtures:", err)
	}
	return b
}
func TestSchemaCopy(t *testing.T) {
	b, err := os.ReadFile("../../examples/schema.json")
	if err != nil || string(b) != string(Schema) {
		t.Fatal("example schema differs from embedded schema")
	}
}
func TestRefusals(t *testing.T) {
	for _, name := range []string{"missing", "duplicate", "conflict", "wrong-interface", "no-route"} {
		t.Run(name, func(t *testing.T) {
			if _, err := New(fixture(t, name)); err == nil {
				t.Fatal("accepted invalid handler registration")
			}
		})
	}
	b := fixture(t, "api")
	b[6] ^= 255
	if _, err := New(b); err == nil {
		t.Fatal("accepted schema mismatch")
	}
	if _, err := New([]byte("DSLB")); err == nil {
		t.Fatal("accepted truncated bytecode")
	}
}
func TestLoopbackOnly(t *testing.T) {
	e, err := New(fixture(t, "api"))
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"0.0.0.0:0", ":0", "localhost:0", "192.0.2.1:0"} {
		if _, err = Start(e, addr); err == nil {
			t.Fatalf("accepted %s", addr)
		}
	}
}
func TestConcurrentState(t *testing.T) {
	e, err := New(fixture(t, "stateful"))
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(e)
	defer s.Close()
	var wg sync.WaitGroup
	out := make(chan string, 40)
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := http.Get(s.URL + "/count")
			if err != nil {
				out <- "ERROR " + err.Error()
				return
			}
			defer r.Body.Close()
			b, _ := io.ReadAll(r.Body)
			out <- string(b)
		}()
	}
	wg.Wait()
	close(out)
	seen := map[string]bool{}
	for v := range out {
		seen[v] = true
	}
	for i := 1; i <= 40; i++ {
		if !seen[fmt.Sprint(i)] {
			t.Fatalf("missing atomic count %d: %v", i, seen)
		}
	}
}
func TestLimitsAndHandlerFailure(t *testing.T) {
	e, err := New(fixture(t, "runtime-error"))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 500 {
		t.Fatalf("want 500, got %d", w.Code)
	}
	e, err = New(fixture(t, "api"))
	if err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/echo", strings.NewReader(strings.Repeat("x", (1<<20)+1))))
	if w.Code != 413 {
		t.Fatalf("want 413, got %d", w.Code)
	}
}
