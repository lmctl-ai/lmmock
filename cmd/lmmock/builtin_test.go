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

// Builtins are virtual registrations: the protocol still validates metadata,
// and dry runs must not reach config(), listener setup, logs, or the client.
func TestBuiltinInvocationDry(t *testing.T) {
	for _, mode := range []string{"exec", "serve"} {
		t.Run(mode, func(t *testing.T) {
			registration, err := filepath.Abs(filepath.Join("builtin", "llm-api.lmcode"))
			if err != nil {
				t.Fatal(err)
			}
			invocationManifest(t, registration, true)
			dir := t.TempDir()
			audit, ready := filepath.Join(dir, "audit"), filepath.Join(dir, "ready")
			args := []string{mode, "--builtin", "llm-api", "--config", filepath.Join(dir, "missing.json"),
				"--listen", "invalid-listen-address", "--audit-log", audit, "--ready-file", ready}
			if mode == "exec" {
				args = append(args, "--", "must-not-execute")
			}
			code, out := invocationOutput(t, args)
			if code != 0 || strings.Count(out, "LMCODE_RETURN_BEGIN") != 1 ||
				!strings.Contains(out, `"ok":true`) || !strings.Contains(out, `"dry":true`) {
				t.Fatalf("dry builtin: %d %s", code, out)
			}
			for _, path := range []string{audit, ready} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("dry created %s: %v", path, err)
				}
			}
		})
	}
}

func TestBuiltinClientProcess(t *testing.T) {
	if os.Getenv("LMMOCK_BUILTIN_CLIENT") != "1" {
		return
	}
	if os.Getenv("LMCODE_INVOCATION_FILE") != "" {
		os.Exit(9)
	}
	res, err := http.Post(os.Getenv("LMMOCK_URL")+"/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"m","messages":[{"role":"user","content":"ping"}]}`))
	if err != nil {
		os.Exit(8)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || !strings.Contains(string(body), "pong") {
		os.Exit(7)
	}
	os.Stdout.WriteString("builtin client passed")
	os.Exit(0)
}

func TestBuiltinExec(t *testing.T) {
	for _, metadata := range []bool{false, true} {
		t.Run(map[bool]string{false: "standalone", true: "invocation"}[metadata], func(t *testing.T) {
			if metadata {
				registration, err := filepath.Abs(filepath.Join("builtin", "llm-api.lmcode"))
				if err != nil {
					t.Fatal(err)
				}
				invocationManifest(t, registration, false)
			}
			t.Setenv("LMMOCK_BUILTIN_CLIENT", "1")
			code, out := invocationOutput(t, []string{"exec", "--builtin", "llm-api", "--",
				os.Args[0], "-test.run=^TestBuiltinClientProcess$"})
			if code != 0 || !strings.Contains(out, "builtin client passed") ||
				strings.Count(out, "LMCODE_RETURN_BEGIN") != 1 || !strings.Contains(out, `"ok":true`) {
				t.Fatalf("builtin exec: %d %s", code, out)
			}
			if metadata && !strings.Contains(out, `"stdout":"builtin client passed"`) {
				t.Fatalf("client output not captured: %s", out)
			}
		})
	}
}

func TestBuiltinRejectsInvalidInvocation(t *testing.T) {
	for _, args := range [][]string{
		{"exec", "--builtin", "llm-api", "--lmcode", "extra.lmcode", "--", "must-not-run"},
		{"exec", "--builtin", "unknown", "--", "must-not-run"},
	} {
		code, out := invocationOutput(t, args)
		if code == 0 || strings.Count(out, "LMCODE_RETURN_BEGIN") != 1 || !strings.Contains(out, `"ok":false`) {
			t.Fatalf("accepted invalid builtin: %d %s", code, out)
		}
	}
	invocationManifest(t, filepath.Join(t.TempDir(), "unrelated.lmcode"), true)
	code, out := invocationOutput(t, []string{"exec", "--builtin", "llm-api", "--", "must-not-run"})
	if code != 2 || !strings.Contains(out, "extra registration") {
		t.Fatalf("ignored registration mismatch: %d %s", code, out)
	}
}

// Claude Code authenticates with Authorization: Bearer (ANTHROPIC_AUTH_TOKEN);
// the Anthropic API accepts that as well as x-api-key.
func TestBuiltinAnthropicAcceptsBearerCredential(t *testing.T) {
	code, err := builtinProgram("llm-api")
	if err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(t.TempDir(), "cfg.json")
	os.WriteFile(cfg, []byte(`{"apiKey":"good-key","toolCalls":false}`), 0600)
	engine, err := mock.NewWithOptions(code, mock.Options{ConfigFile: cfg})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(engine)
	defer server.Close()
	post := func(header, value string) int {
		req, _ := http.NewRequest("POST", server.URL+"/v1/messages", strings.NewReader(`{"model":"m","max_tokens":5,"messages":[{"role":"user","content":"hi"}]}`))
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("content-type", "application/json")
		req.Header.Set(header, value)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, c := range []struct {
		header, value string
		want          int
	}{{"x-api-key", "good-key", 200}, {"Authorization", "Bearer good-key", 200}, {"Authorization", "Bearer bad", 401}, {"x-api-key", "bad", 401}} {
		if got := post(c.header, c.value); got != c.want {
			t.Errorf("%s %q: got %d want %d", c.header, c.value, got, c.want)
		}
	}
}
