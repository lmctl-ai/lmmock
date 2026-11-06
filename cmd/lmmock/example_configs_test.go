package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

// Every ready-made configuration under examples/ must do what its name says
// against the embedded builtin it is documented for.

type exampleServer struct {
	t   *testing.T
	srv *httptest.Server
}

func startExample(t *testing.T, builtin, config string) *exampleServer {
	t.Helper()
	code, err := builtinProgram(builtin)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := mock.NewWithOptions(code, mock.Options{ConfigFile: filepath.Join("..", "..", "examples", config)})
	if err != nil {
		t.Fatal(err)
	}
	s := &exampleServer{t: t, srv: httptest.NewServer(engine)}
	t.Cleanup(s.srv.Close)
	return s
}

func (s *exampleServer) do(method, path, body string, headers map[string]string) (int, string, http.Header) {
	s.t.Helper()
	req, _ := http.NewRequest(method, s.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), res.Header
}

const anthropicWithTool = `{"model":"m","max_tokens":8,"messages":[{"role":"user","content":"hi"}],"tools":[{"name":"lookup","input_schema":{"type":"object"}}]}`

func chat(turns ...string) string {
	var msgs []string
	for _, turn := range turns {
		msgs = append(msgs, `{"role":"user","content":`+quote(turn)+`}`)
	}
	return `{"model":"m","messages":[` + strings.Join(msgs, ",") + `]}`
}

func quote(s string) string { b, _ := json.Marshal(s); return string(b) }

func TestExampleCodingCLIConfig(t *testing.T) {
	s := startExample(t, "llm-api", "llm-config-coding-cli.json")
	anthropic := map[string]string{"anthropic-version": "2023-06-01"}
	for _, c := range []struct {
		name, header, value string
		want                int
	}{
		{"auth token (ANTHROPIC_AUTH_TOKEN)", "Authorization", "Bearer fixture-key", 200},
		{"api key header", "x-api-key", "fixture-key", 200},
		{"wrong token", "Authorization", "Bearer nope", 401},
	} {
		h := map[string]string{"anthropic-version": anthropic["anthropic-version"], c.header: c.value}
		code, body, _ := s.do("POST", "/v1/messages", anthropicWithTool, h)
		if code != c.want {
			t.Errorf("%s: got %d want %d: %s", c.name, code, c.want, body)
		}
		if code == 200 && (!strings.Contains(body, "pong") || strings.Contains(body, "tool_use")) {
			t.Errorf("%s: want a plain pong despite declared tools: %s", c.name, body)
		}
	}
}

func TestExampleScriptedTurnsConfig(t *testing.T) {
	s := startExample(t, "llm-api", "llm-config-scripted-turns.json")
	first, second, total := "The first number is 3.", "The second number is 7.", "What is the total?"
	for _, c := range []struct {
		turns []string
		want  string
	}{
		{[]string{first}, "Noted."},
		{[]string{first, second}, "Noted."},
		{[]string{first, second, total}, "10"},
	} {
		code, body, _ := s.do("POST", "/v1/chat/completions", chat(c.turns...), nil)
		if code != 200 || !strings.Contains(body, c.want) {
			t.Errorf("%d turns: %d %s (want %q)", len(c.turns), code, body, c.want)
		}
	}
}

func TestExampleFaultConfigs(t *testing.T) {
	msgs := map[string]string{"anthropic-version": "2023-06-01"}
	s := startExample(t, "llm-api", "llm-config-rate-limit.json")
	if code, _, h := s.do("POST", "/v1/messages", anthropicWithTool, msgs); code != 429 || h.Get("Retry-After") == "" {
		t.Errorf("rate-limit: %d retry-after=%q", code, h.Get("Retry-After"))
	}
	s = startExample(t, "llm-api", "llm-config-server-error.json")
	if code, _, _ := s.do("POST", "/v1/messages", anthropicWithTool, msgs); code != 529 {
		t.Errorf("server-error: %d", code)
	}
	s = startExample(t, "llm-api", "llm-config-unauthorized.json")
	if code, _, _ := s.do("POST", "/v1/messages", anthropicWithTool, msgs); code != 401 {
		t.Errorf("unauthorized: %d", code)
	}
	s = startExample(t, "llm-api", "llm-config-retry.json")
	first, _, _ := s.do("POST", "/v1/chat/completions", chat("hi"), nil)
	second, _, _ := s.do("POST", "/v1/chat/completions", chat("hi"), nil)
	if first != 429 || second != 200 {
		t.Errorf("retry: first %d then %d, want 429 then 200", first, second)
	}
}

func deviceLogin(s *exampleServer) (id, user string) {
	s.t.Helper()
	code, body, _ := s.do("POST", "/api/accounts/deviceauth/usercode", `{"client_id":"app"}`, nil)
	var out struct {
		ID   string `json:"device_auth_id"`
		User string `json:"user_code"`
	}
	if code != 200 || json.Unmarshal([]byte(body), &out) != nil || out.ID == "" {
		s.t.Fatalf("usercode: %d %s", code, body)
	}
	return out.ID, out.User
}

func poll(s *exampleServer, id, user string) (int, string) {
	code, body, _ := s.do("POST", "/api/accounts/deviceauth/token", `{"device_auth_id":`+quote(id)+`,"user_code":`+quote(user)+`}`, nil)
	return code, body
}

func TestExampleCodexDeviceLoginConfigs(t *testing.T) {
	s := startExample(t, "oauth-codex-device", "oauth-codex-device-slow-approval.json")
	id, user := deviceLogin(s)
	for i := 0; i < 3; i++ {
		if code, _ := poll(s, id, user); code != 403 {
			t.Fatalf("slow approval poll %d: %d, want 403 (pending)", i+1, code)
		}
	}
	if code, body := poll(s, id, user); code != 200 || !strings.Contains(body, "authorization_code") {
		t.Fatalf("slow approval: %d %s", code, body)
	}

	s = startExample(t, "oauth-codex-device", "oauth-codex-device-manual-approval.json")
	id, user = deviceLogin(s)
	if code, _ := poll(s, id, user); code != 403 {
		t.Fatalf("manual approval before approve: %d", code)
	}
	if code, body, _ := s.do("POST", "/__oauth/approve", `{"user_code":`+quote(user)+`}`, nil); code/100 != 2 {
		t.Fatalf("approve: %d %s", code, body)
	}
	if code, _ := poll(s, id, user); code != 200 {
		t.Fatalf("manual approval after approve: %d", code)
	}

	s = startExample(t, "oauth-codex-device", "oauth-codex-device-denied.json")
	id, user = deviceLogin(s)
	if code, body := poll(s, id, user); code != 400 || !strings.Contains(body, "access_denied") {
		t.Fatalf("denied: %d %s", code, body)
	}

	s = startExample(t, "oauth-codex-device", "oauth-codex-device-expired.json")
	id, user = deviceLogin(s)
	if code, body := poll(s, id, user); code != 410 || !strings.Contains(body, "expired_token") {
		t.Fatalf("expired: %d %s", code, body)
	}
}
