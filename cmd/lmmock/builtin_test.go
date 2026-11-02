package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

func post(t *testing.T, url, body string) string {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != 200 {
		t.Fatalf("%s: %d %s", url, res.StatusCode, b)
	}
	return string(b)
}

func TestBuiltinLLMAPI(t *testing.T) {
	code, err := builtinProgram("llm-api")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := builtinProgram("nope"); err == nil {
		t.Fatal("accepted unknown builtin")
	}
	cfg := filepath.Join(t.TempDir(), "cfg.json")
	run := func(config string) *httptest.Server {
		if config != "" {
			if err := os.WriteFile(cfg, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
		}
		engine, err := mock.NewWithOptions(code, mock.Options{ConfigFile: map[bool]string{true: cfg}[config != ""]})
		if err != nil {
			t.Fatal(err)
		}
		return httptest.NewServer(engine)
	}
	const msg = `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"ping"}],` +
		`"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`

	srv := run("")
	defer srv.Close()
	if body := post(t, srv.URL+"/v1/messages", msg); !strings.Contains(body, "tool_use") {
		t.Fatalf("default config should call the offered tool: %s", body)
	}
	res, err := http.Get(srv.URL + "/__requests")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(b), `"count":1`) {
		t.Fatalf("__requests: %s", b)
	}

	off := run(`{"toolCalls":false}`)
	defer off.Close()
	for _, c := range []struct{ path, body string }{
		{"/v1/messages", msg},
		{"/v1/chat/completions", `{"model":"m","messages":[{"role":"user","content":"ping"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`},
		{"/v1/responses", `{"model":"m","input":"ping","tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`},
		{"/v1beta/models/m:generateContent", `{"contents":[{"role":"user","parts":[{"text":"ping"}]}],"tools":[{"functionDeclarations":[{"name":"lookup"}]}]}`},
		{"/api/chat", `{"model":"m","stream":false,"messages":[{"role":"user","content":"ping"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`},
	} {
		body := post(t, off.URL+c.path, c.body)
		if !strings.Contains(body, "pong") || strings.Contains(body, "lookup") {
			t.Fatalf("%s with toolCalls=false: %s", c.path, body)
		}
	}
}
