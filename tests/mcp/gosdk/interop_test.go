package gosdk_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	if r := os.Getenv("LMMOCK_ROOT"); r != "" {
		return r
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "../../.."))
}

func lmmockPaths(t *testing.T) (string, string) {
	t.Helper()
	root := repoRoot(t)
	bin := os.Getenv("LMMOCK_BIN")
	if bin == "" {
		bin = filepath.Join(root, "build", "lmmock")
	}
	lmcodePath := os.Getenv("LMMOCK_MCP_LMCODE")
	if lmcodePath == "" {
		lmcodePath = filepath.Join(root, "build", "mcp", "server.lmcode")
	}
	return bin, lmcodePath
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startMockServer(t *testing.T, protocolVersion string) string {
	t.Helper()
	bin, lmcodePath := lmmockPaths(t)
	addr := freeLoopbackAddr(t)
	cfgDir := t.TempDir()
	cfgPath := filepath.Join(cfgDir, "config.json")
	cfgJSON := fmt.Sprintf(`{"protocolVersion":%q}`, protocolVersion)
	if err := os.WriteFile(cfgPath, []byte(cfgJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, "serve", "--lmcode", lmcodePath, "--listen", addr, "--config", cfgPath)
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("start lmmock: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	baseURL := "http://" + addr
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/__mock/state")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return baseURL
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("lmmock server at %s did not become ready", baseURL)
	return ""
}

func setReplyMode(t *testing.T, baseURL, mode string) {
	t.Helper()
	resp, err := http.Post(
		baseURL+"/__mock/reply-mode",
		"application/json",
		strings.NewReader(fmt.Sprintf(`{"mode":%q}`, mode)),
	)
	if err != nil {
		t.Fatalf("set reply-mode %s: %v", mode, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("set reply-mode %s status=%d body=%s", mode, resp.StatusCode, string(body))
	}
}

func resolveDef(t *testing.T, schemaFile, defsKey, defName string, disallowExtra bool) *jsonschema.Resolved {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "tests", "mcp", "schema", schemaFile))
	if err != nil {
		t.Fatal(err)
	}
	var root jsonschema.Schema
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("unmarshal %s: %v", schemaFile, err)
	}
	if disallowExtra {
		defs := root.Defs
		if defsKey == "definitions" {
			defs = root.Definitions
		}
		for _, d := range defs {
			if d != nil && len(d.Properties) > 0 && d.AdditionalProperties == nil {
				d.AdditionalProperties = &jsonschema.Schema{Not: &jsonschema.Schema{}}
			}
		}
	}
	root.Ref = fmt.Sprintf("#/%s/%s", defsKey, defName)
	resolved, err := root.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve %s #/%s/%s: %v", schemaFile, defsKey, defName, err)
	}
	return resolved
}

func validateJSONBytes(t *testing.T, r *jsonschema.Resolved, raw []byte, label string) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("%s: invalid JSON %q: %v", label, string(raw), err)
	}
	if err := r.Validate(v); err != nil {
		t.Fatalf("%s: schema validation failed: %v\npayload: %s", label, err, string(raw))
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%s: expected JSON object, got %T", label, v)
	}
	return m
}

func TestSchemaValidatorRejectsInvalidPayloadsAndValidatesWireResponses(t *testing.T) {
	msg2026 := resolveDef(t, "2026-07-28.schema.json", "$defs", "JSONRPCMessage", false)
	if err := msg2026.Validate("not-an-object"); err == nil {
		t.Fatal("validator accepted primitive string as JSONRPCMessage")
	}
	if err := msg2026.Validate(map[string]any{"jsonrpc": "999.0", "id": 1, "result": map[string]any{"resultType": "complete"}}); err == nil {
		t.Fatal("validator accepted jsonrpc 999.0")
	}
	if err := msg2026.Validate(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}}); err == nil {
		t.Fatal("validator accepted 2026-07-28 result without required resultType")
	}

	// Validate 2026-07-28 wire responses against strict DiscoverResult, ListToolsResult, CallToolResult
	base2026 := startMockServer(t, "2026-07-28")
	discoverDef2026 := resolveDef(t, "2026-07-28.schema.json", "$defs", "DiscoverResult", true)
	reqBody := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	req, _ := http.NewRequest("POST", base2026+"/mcp", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", "2026-07-28")
	req.Header.Set("Mcp-Method", "server/discover")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	env := validateJSONBytes(t, msg2026, raw, "2026 server/discover envelope")
	resBytes, _ := json.Marshal(env["result"])
	validateJSONBytes(t, discoverDef2026, resBytes, "2026 DiscoverResult")

	// Validate 2025-11-25 wire responses do not contain 2026-07-28-only properties (resultType, cacheScope, ttlMs)
	base2025 := startMockServer(t, "2025-11-25")
	msg2025 := resolveDef(t, "2025-11-25.schema.json", "$defs", "JSONRPCMessage", false)
	listTools2025 := resolveDef(t, "2025-11-25.schema.json", "$defs", "ListToolsResult", true)

	initReq, _ := http.NewRequest("POST", base2025+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Header.Set("Accept", "application/json, text/event-stream")
	initReq.Header.Set("MCP-Protocol-Version", "2025-11-25")
	initResp, err := http.DefaultClient.Do(initReq)
	if err != nil {
		t.Fatal(err)
	}
	sid := initResp.Header.Get("Mcp-Session-Id")
	_ = initResp.Body.Close()

	toolsReq, _ := http.NewRequest("POST", base2025+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	toolsReq.Header.Set("Content-Type", "application/json")
	toolsReq.Header.Set("Accept", "application/json, text/event-stream")
	toolsReq.Header.Set("MCP-Protocol-Version", "2025-11-25")
	toolsReq.Header.Set("Mcp-Session-Id", sid)
	toolsResp, err := http.DefaultClient.Do(toolsReq)
	if err != nil {
		t.Fatal(err)
	}
	toolsRaw, _ := io.ReadAll(toolsResp.Body)
	_ = toolsResp.Body.Close()
	env2025 := validateJSONBytes(t, msg2025, toolsRaw, "2025 tools/list envelope")
	res2025Bytes, _ := json.Marshal(env2025["result"])
	validateJSONBytes(t, listTools2025, res2025Bytes, "2025 strict ListToolsResult")
}

func TestGoSDK2026Stateless(t *testing.T) {
	for _, mode := range []string{"json", "sse"} {
		t.Run(mode, func(t *testing.T) {
			baseURL := startMockServer(t, "2026-07-28")
			setReplyMode(t, baseURL, mode)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var toolsChanged atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "gosdk-2026", Version: "1.0.0"}, &mcp.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
					toolsChanged.Add(1)
				},
			})
			transport := &mcp.StreamableClientTransport{
				Endpoint:   baseURL + "/mcp",
				MaxRetries: -1,
			}
			session, err := client.Connect(ctx, transport, nil)
			if err != nil {
				t.Fatalf("Connect failed: %v", err)
			}
			defer session.Close()

			initRes := session.InitializeResult()
			if initRes == nil || initRes.ProtocolVersion != "2026-07-28" {
				t.Fatalf("unexpected InitializeResult: %+v", initRes)
			}
			if initRes.ServerInfo == nil || initRes.ServerInfo.Name != "lmmock-mcp" {
				t.Fatalf("expected ServerInfo.Name=lmmock-mcp in DiscoverResult._meta, got %+v", initRes.ServerInfo)
			}

			toolsRes, err := session.ListTools(ctx, nil)
			if err != nil || len(toolsRes.Tools) != 6 {
				t.Fatalf("ListTools: %v, len=%d", err, len(toolsRes.Tools))
			}

			callRes, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "echo",
				Arguments: map[string]any{"text": "hello-" + mode},
			})
			if err != nil || callRes.IsError {
				t.Fatalf("CallTool echo: %v", err)
			}

			addRes, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "add",
				Arguments: map[string]any{"left": 12, "right": 30},
			})
			if err != nil || addRes.IsError {
				t.Fatalf("CallTool add: %v", err)
			}

			// Non-ASCII resource URI triggers go-sdk Mcp-Name: =?base64?...?= encoding
			unicodeRes, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "mock://item/héllo"})
			if err != nil || len(unicodeRes.Contents) != 1 || unicodeRes.Contents[0].URI != "mock://item/héllo" {
				t.Fatalf("ReadResource non-ASCII URI failed: %v, res=%+v", err, unicodeRes)
			}

			promptRes, err := session.GetPrompt(ctx, &mcp.GetPromptParams{
				Name:      "greet",
				Arguments: map[string]string{"name": "Curie"},
			})
			if err != nil || len(promptRes.Messages) != 1 {
				t.Fatalf("GetPrompt failed: %v", err)
			}

			// Trigger list-changed and verify subscriptions/listen delivered the notification
			time.Sleep(50 * time.Millisecond)
			resp, err := http.Post(baseURL+"/__mock/list-changed", "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && toolsChanged.Load() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if toolsChanged.Load() < 1 {
				t.Fatalf("expected ToolListChangedHandler to fire via subscriptions/listen, got %d", toolsChanged.Load())
			}
		})
	}
}

func TestGoSDK2025Session(t *testing.T) {
	for _, mode := range []string{"json", "sse"} {
		t.Run(mode, func(t *testing.T) {
			baseURL := startMockServer(t, "2025-11-25")
			setReplyMode(t, baseURL, mode)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var toolsChanged atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "gosdk-2025", Version: "1.0.0"}, &mcp.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
					toolsChanged.Add(1)
				},
			})
			httpClient := &http.Client{
				Transport: &http.Transport{
					Proxy:                 nil,
					ResponseHeaderTimeout: 2 * time.Second,
				},
			}
			transport := &mcp.StreamableClientTransport{
				Endpoint:   baseURL + "/mcp",
				HTTPClient: httpClient,
				MaxRetries: -1,
			}
			session, err := client.Connect(ctx, transport, nil)
			if err != nil {
				t.Fatalf("2025-11-25 Connect failed: %v", err)
			}
			defer session.Close()

			if session.ID() == "" {
				t.Fatal("expected non-empty session ID in 2025-11-25 mode")
			}
			if err := session.Ping(ctx, nil); err != nil {
				t.Fatalf("Ping failed: %v", err)
			}
			toolsRes, err := session.ListTools(ctx, nil)
			if err != nil || len(toolsRes.Tools) != 6 {
				t.Fatalf("ListTools: %v, len=%d", err, len(toolsRes.Tools))
			}
			callRes, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "echo",
				Arguments: map[string]any{"text": "session-" + mode},
			})
			if err != nil || callRes.IsError {
				t.Fatalf("CallTool echo: %v", err)
			}

			resp, err := http.Post(baseURL+"/__mock/list-changed", "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && toolsChanged.Load() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if toolsChanged.Load() < 1 {
				t.Fatalf("expected ToolListChangedHandler to fire over GET /mcp SSE, got %d", toolsChanged.Load())
			}
		})
	}
}

func TestGoSDK2024LegacySSE(t *testing.T) {
	for _, mode := range []string{"json", "sse"} {
		t.Run(mode, func(t *testing.T) {
			baseURL := startMockServer(t, "2024-11-05")
			setReplyMode(t, baseURL, mode)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			var toolsChanged atomic.Int32
			client := mcp.NewClient(&mcp.Implementation{Name: "gosdk-2024", Version: "1.0.0"}, &mcp.ClientOptions{
				ToolListChangedHandler: func(context.Context, *mcp.ToolListChangedRequest) {
					toolsChanged.Add(1)
				},
			})
			transport := &mcp.SSEClientTransport{
				Endpoint: baseURL + "/sse",
			}
			session, err := client.Connect(ctx, transport, nil)
			if err != nil {
				t.Fatalf("2024-11-05 SSE Connect failed: %v", err)
			}
			defer session.Close()

			if err := session.Ping(ctx, nil); err != nil {
				t.Fatalf("Ping failed: %v", err)
			}
			toolsRes, err := session.ListTools(ctx, nil)
			if err != nil || len(toolsRes.Tools) != 6 {
				t.Fatalf("ListTools: %v, len=%d", err, len(toolsRes.Tools))
			}
			callRes, err := session.CallTool(ctx, &mcp.CallToolParams{
				Name:      "echo",
				Arguments: map[string]any{"text": "sse-" + mode},
			})
			if err != nil || callRes.IsError {
				t.Fatalf("CallTool echo: %v", err)
			}

			resp, err := http.Post(baseURL+"/__mock/list-changed", "application/json", bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && toolsChanged.Load() == 0 {
				time.Sleep(20 * time.Millisecond)
			}
			if toolsChanged.Load() < 1 {
				t.Fatalf("expected ToolListChangedHandler to fire over GET /sse, got %d", toolsChanged.Load())
			}
		})
	}
}

func TestTLSSameOriginGuard(t *testing.T) {
	root := repoRoot(t)
	bin, compiled := lmmockPaths(t)
	cfg := filepath.Join(root, "examples", "mcp", "streamable-http", "config.json")
	addr := freeLoopbackAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, bin, "serve", "--tls", "--lmcode", compiled, "--config", cfg, "--listen", addr)
	cmd.Dir = root
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		_ = cmd.Wait()
	})

	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	hc := &http.Client{Transport: tr, Timeout: 2 * time.Second}
	host := addr
	baseURL := "https://" + host

	deadline := time.Now().Add(5 * time.Second)
	for {
		resp, err := hc.Get(baseURL + "/__mock/state")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("TLS server failed to become ready on %s", baseURL)
		}
		time.Sleep(15 * time.Millisecond)
	}

	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}}`
	reqOK, _ := http.NewRequest("POST", baseURL+"/mcp", strings.NewReader(body))
	reqOK.Header.Set("Content-Type", "application/json")
	reqOK.Header.Set("Accept", "application/json, text/event-stream")
	reqOK.Header.Set("MCP-Protocol-Version", "2026-07-28")
	reqOK.Header.Set("Mcp-Method", "server/discover")
	reqOK.Header.Set("Origin", baseURL)
	respOK, err := hc.Do(reqOK)
	if err != nil {
		t.Fatal(err)
	}
	_ = respOK.Body.Close()
	if respOK.StatusCode != 200 {
		t.Fatalf("expected 200 for same-origin https:// Origin under --tls, got %d", respOK.StatusCode)
	}

	reqBad, _ := http.NewRequest("POST", baseURL+"/mcp", strings.NewReader(body))
	reqBad.Header.Set("Content-Type", "application/json")
	reqBad.Header.Set("Accept", "application/json, text/event-stream")
	reqBad.Header.Set("MCP-Protocol-Version", "2026-07-28")
	reqBad.Header.Set("Mcp-Method", "server/discover")
	reqBad.Header.Set("Origin", "http://"+host)
	respBad, err := hc.Do(reqBad)
	if err != nil {
		t.Fatal(err)
	}
	_ = respBad.Body.Close()
	if respBad.StatusCode != 403 {
		t.Fatalf("expected 403 for http:// Origin against https:// --tls server, got %d", respBad.StatusCode)
	}
}
