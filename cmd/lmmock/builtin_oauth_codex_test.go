package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

type codexMock struct {
	t   *testing.T
	srv *httptest.Server
}

func newCodexMock(t *testing.T, config string) *codexMock {
	t.Helper()
	code, err := builtinProgram("oauth-codex-device")
	if err != nil {
		t.Fatal(err)
	}
	opts := mock.Options{}
	if config != "" {
		opts.ConfigFile = filepath.Join(t.TempDir(), "cfg.json")
		if err := os.WriteFile(opts.ConfigFile, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
	}
	engine, err := mock.NewWithOptions(code, opts)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(engine)
	t.Cleanup(srv.Close)
	return &codexMock{t, srv}
}

func (m *codexMock) do(path, contentType, body string) (int, map[string]any, string) {
	m.t.Helper()
	res, err := http.Post(m.srv.URL+path, contentType, strings.NewReader(body))
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	return res.StatusCode, obj, string(raw)
}

func (m *codexMock) json(path string, v map[string]any) (int, map[string]any, string) {
	b, _ := json.Marshal(v)
	return m.do(path, "application/json", string(b))
}

func (m *codexMock) form(v url.Values) (int, map[string]any, string) {
	return m.do("/oauth/token", "application/x-www-form-urlencoded", v.Encode())
}

const (
	codexClient = "app_EMoamEEZ73f0CkXaXp7hrann"
	deviceBase  = "/api/accounts/deviceauth/"
)

func (m *codexMock) usercode() (id, user string) {
	m.t.Helper()
	st, o, raw := m.json(deviceBase+"usercode", map[string]any{"client_id": codexClient})
	if st != 200 {
		m.t.Fatalf("usercode: %d %s", st, raw)
	}
	return o["device_auth_id"].(string), o["user_code"].(string)
}

func (m *codexMock) poll(id, user string) (int, map[string]any) {
	st, o, _ := m.json(deviceBase+"token", map[string]any{"device_auth_id": id, "user_code": user})
	return st, o
}

func (m *codexMock) exchange(poll map[string]any, verifier string) (int, map[string]any, string) {
	return m.form(url.Values{
		"grant_type": {"authorization_code"}, "code": {poll["authorization_code"].(string)},
		"redirect_uri": {"https://auth.openai.com/deviceauth/callback"}, "client_id": {codexClient},
		"code_verifier": {verifier},
	})
}

func (m *codexMock) state() (counts map[string]float64, steps []string) {
	m.t.Helper()
	res, err := http.Get(m.srv.URL + "/__oauth")
	if err != nil {
		m.t.Fatal(err)
	}
	defer res.Body.Close()
	var s struct {
		Counts map[string]float64 `json:"counts"`
		Events []struct {
			Step   string `json:"step"`
			Status int    `json:"status"`
		} `json:"events"`
	}
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		m.t.Fatal(err)
	}
	for _, e := range s.Events {
		steps = append(steps, e.Step+":"+itoa(e.Status))
	}
	return s.Counts, steps
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

// jwtClaims decodes the payload exactly like codex's decode_jwt_payload.
func jwtClaims(t *testing.T, tok string) map[string]any {
	t.Helper()
	parts := strings.Split(tok, ".")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		t.Fatalf("not a JWT: %d parts", len(parts))
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var c map[string]any
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBuiltinOAuthCodexDeviceHappyPath(t *testing.T) {
	m := newCodexMock(t, "")
	id, user := m.usercode()
	st, p := m.poll(id, user)
	if st != 200 {
		t.Fatalf("poll: %d", st)
	}
	challenge := p["code_challenge"].(string)
	sum := sha256.Sum256([]byte(p["code_verifier"].(string)))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
		t.Fatal("code_challenge is not S256(code_verifier)")
	}
	st, tok, raw := m.exchange(p, p["code_verifier"].(string))
	if st != 200 {
		t.Fatalf("exchange: %d %s", st, raw)
	}
	idClaims := jwtClaims(t, tok["id_token"].(string))
	auth := idClaims["https://api.openai.com/auth"].(map[string]any)
	for _, k := range []string{"chatgpt_plan_type", "chatgpt_user_id", "chatgpt_account_id"} {
		if s, _ := auth[k].(string); s == "" {
			t.Fatalf("id_token auth claim %s missing", k)
		}
	}
	if idClaims["email"] == "" || jwtClaims(t, tok["access_token"].(string))["exp"] == nil || tok["refresh_token"] == "" {
		t.Fatalf("incomplete tokens: %v", idClaims)
	}
	// authorization codes are single use
	if st, _, _ := m.exchange(p, p["code_verifier"].(string)); st != 400 {
		t.Fatalf("code reuse accepted: %d", st)
	}
	// API-key token exchange
	st, key, raw := m.form(url.Values{
		"grant_type":         {"urn:ietf:params:oauth:grant-type:token-exchange"},
		"client_id":          {codexClient},
		"requested_token":    {"openai-api-key"},
		"subject_token":      {tok["id_token"].(string)},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
	})
	if st != 200 || !strings.HasPrefix(key["access_token"].(string), "sk-mock-") {
		t.Fatalf("api key exchange: %d %s", st, raw)
	}
	st, _, _ = m.form(url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "client_id": {codexClient},
		"requested_token": {"openai-api-key"}, "subject_token": {"forged.jwt.value"},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"},
	})
	if st != 400 {
		t.Fatalf("forged subject token accepted: %d", st)
	}
	counts, steps := m.state()
	if counts["usercode"] != 1 || counts["poll"] != 1 || counts["api_key_exchange"] != 2 {
		t.Fatalf("counts: %v", counts)
	}
	if strings.Join(steps[:3], ",") != "usercode:200,poll:200,code_exchange:200" {
		t.Fatalf("steps: %v", steps)
	}
	// codex deserializes interval as a string and rejects a JSON number
	if _, o, _ := m.json(deviceBase+"usercode", map[string]any{"client_id": codexClient}); o["interval"] != "1" {
		t.Fatalf("interval must be a JSON string: %v", o["interval"])
	}
}

func TestBuiltinOAuthCodexDevicePendingThenApproved(t *testing.T) {
	m := newCodexMock(t, `{"pollsBeforeApproval":2,"intervalAsNumber":true,"userCode":"TEST-1234"}`)
	_, o, raw := m.json(deviceBase+"usercode", map[string]any{"client_id": codexClient})
	if o["interval"] != float64(1) || o["user_code"] != "TEST-1234" {
		t.Fatalf("usercode: %s", raw)
	}
	id := o["device_auth_id"].(string)
	for i := 0; i < 2; i++ {
		if st, _ := m.poll(id, "TEST-1234"); st != 403 {
			t.Fatalf("poll %d: %d", i, st)
		}
	}
	if st, _ := m.poll(id, "wrong"); st != 400 {
		t.Fatalf("wrong user code: %d", st)
	}
	if st, p := m.poll(id, "TEST-1234"); st != 200 || p["authorization_code"] == nil {
		t.Fatalf("third poll: %d", st)
	}
}

func TestBuiltinOAuthCodexDeviceManualApproval(t *testing.T) {
	m := newCodexMock(t, `{"autoApprove":false,"pendingStatus":404}`)
	id, user := m.usercode()
	if st, _ := m.poll(id, user); st != 404 {
		t.Fatalf("pending: %d", st)
	}
	if st, _, _ := m.json("/__oauth/approve", map[string]any{"user_code": "nope"}); st != 404 {
		t.Fatalf("approve unknown: %d", st)
	}
	if st, _, _ := m.json("/__oauth/approve", map[string]any{"user_code": user}); st != 200 {
		t.Fatalf("approve: %d", st)
	}
	if st, _ := m.poll(id, user); st != 200 {
		t.Fatalf("approved poll: %d", st)
	}
}

func TestBuiltinOAuthCodexDeviceModes(t *testing.T) {
	for mode, want := range map[string]int{"pending-forever": 403, "denied": 400, "expired": 410} {
		m := newCodexMock(t, `{"mode":"`+mode+`"}`)
		id, user := m.usercode()
		for i := 0; i < 2; i++ {
			if st, _ := m.poll(id, user); st != want {
				t.Fatalf("%s: %d want %d", mode, st, want)
			}
		}
	}
	m := newCodexMock(t, `{"disabled":true}`)
	if st, _, _ := m.json(deviceBase+"usercode", map[string]any{"client_id": codexClient}); st != 404 {
		t.Fatalf("disabled: %d", st)
	}
	m = newCodexMock(t, `{"clientId":"only-this"}`)
	if st, _, _ := m.json(deviceBase+"usercode", map[string]any{"client_id": codexClient}); st != 400 {
		t.Fatalf("client mismatch: %d", st)
	}
}

func TestBuiltinOAuthCodexDeviceRejectsBadVerifier(t *testing.T) {
	m := newCodexMock(t, "")
	id, user := m.usercode()
	_, p := m.poll(id, user)
	for _, v := range []string{"", "wrong-verifier-value-that-is-long-enough-1234567890"} {
		st, body, raw := m.exchange(p, v)
		if st != 400 || body["error"] != "invalid_grant" || strings.Contains(raw, p["authorization_code"].(string)) {
			t.Fatalf("bad verifier %q: %d %s", v, st, raw)
		}
	}
	// the failed attempts consumed the code
	if st, _, _ := m.exchange(p, p["code_verifier"].(string)); st != 400 {
		t.Fatalf("code survived a failed exchange: %d", st)
	}
	// wrong redirect URI
	_, p = m.poll(id, user)
	st, _, _ := m.form(url.Values{"grant_type": {"authorization_code"}, "code": {p["authorization_code"].(string)},
		"redirect_uri": {"https://x.test/other"}, "client_id": {codexClient}, "code_verifier": {p["code_verifier"].(string)}})
	if st != 400 {
		t.Fatalf("wrong redirect accepted: %d", st)
	}
}

func TestBuiltinOAuthCodexDeviceRefreshRotates(t *testing.T) {
	m := newCodexMock(t, `{"failApiKeyExchange":true}`)
	id, user := m.usercode()
	_, p := m.poll(id, user)
	_, tok, _ := m.exchange(p, p["code_verifier"].(string))
	first := tok["refresh_token"].(string)
	// codex sends refresh as JSON
	st, next, raw := m.json("/oauth/token", map[string]any{"client_id": codexClient, "grant_type": "refresh_token", "refresh_token": first})
	if st != 200 || next["refresh_token"] == first || next["refresh_token"] == "" || next["id_token"] == nil {
		t.Fatalf("refresh: %d %s", st, raw)
	}
	// replay of the rotated token is reported the way codex classifies it
	st, body, _ := m.json("/oauth/token", map[string]any{"client_id": codexClient, "grant_type": "refresh_token", "refresh_token": first})
	if st != 401 || body["error"] != "refresh_token_reused" {
		t.Fatalf("replay: %d %v", st, body)
	}
	// form encoding works too, and unknown tokens are invalid_grant-like
	st, _, _ = m.form(url.Values{"grant_type": {"refresh_token"}, "client_id": {codexClient}, "refresh_token": {next["refresh_token"].(string)}})
	if st != 200 {
		t.Fatalf("form refresh: %d", st)
	}
	if st, _, _ = m.form(url.Values{"grant_type": {"refresh_token"}, "client_id": {codexClient}, "refresh_token": {"never-issued"}}); st != 401 {
		t.Fatalf("unknown refresh token: %d", st)
	}
	// logout revokes the refresh token the way codex does
	if st, _, _ := m.json("/oauth/revoke", map[string]any{"client_id": codexClient, "token": next["refresh_token"], "token_type_hint": "refresh_token"}); st != 200 {
		t.Fatalf("revoke: %d", st)
	}
	st, body, _ = m.json("/oauth/token", map[string]any{"client_id": codexClient, "grant_type": "refresh_token", "refresh_token": next["refresh_token"]})
	if st != 401 || body["error"] != "refresh_token_invalidated" {
		t.Fatalf("revoked token: %d %v", st, body)
	}
	// failApiKeyExchange is tolerated by codex
	if st, _, _ = m.form(url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:token-exchange"}, "client_id": {codexClient},
		"requested_token": {"openai-api-key"}, "subject_token": {tok["id_token"].(string)},
		"subject_token_type": {"urn:ietf:params:oauth:token-type:id_token"}}); st != 400 {
		t.Fatalf("failApiKeyExchange: %d", st)
	}
	if c, _ := m.state(); c["refresh"] != 5 || c["revoke"] != 1 {
		t.Fatalf("refresh count: %v", c)
	}
}

func TestBuiltinOAuthCodexDeviceNonRotating(t *testing.T) {
	m := newCodexMock(t, `{"rotateRefreshTokens":false,"expiresIn":-60}`)
	id, user := m.usercode()
	_, p := m.poll(id, user)
	_, tok, _ := m.exchange(p, p["code_verifier"].(string))
	if exp := jwtClaims(t, tok["access_token"].(string))["exp"].(float64); exp > float64(1<<40) || exp <= 0 {
		t.Fatalf("exp: %v", exp)
	}
	rt := tok["refresh_token"].(string)
	for i := 0; i < 2; i++ {
		_, n, _ := m.json("/oauth/token", map[string]any{"client_id": codexClient, "grant_type": "refresh_token", "refresh_token": rt})
		if n["refresh_token"] != rt {
			t.Fatalf("token rotated: %v", n)
		}
	}
}

func TestBuiltinOAuthCodexDeviceCorruptVerifier(t *testing.T) {
	m := newCodexMock(t, `{"corruptVerifier":true}`)
	id, user := m.usercode()
	_, p := m.poll(id, user)
	if st, body, _ := m.exchange(p, p["code_verifier"].(string)); st != 400 || body["error"] != "invalid_grant" {
		t.Fatalf("corrupt verifier accepted: %d %v", st, body)
	}
}
