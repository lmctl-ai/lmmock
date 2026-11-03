package mock

import (
	"bytes"
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
	"time"
)

type cloudClient struct {
	t   *testing.T
	url string
}

func cloudServer(t *testing.T, config string) cloudClient {
	t.Helper()
	options := Options{}
	if config != "" {
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		options.ConfigFile = p
	}
	engine, err := NewWithOptions(fixture(t, "jira-xray"), options)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(engine)
	t.Cleanup(server.Close)
	return cloudClient{t, server.URL}
}
func (c cloudClient) request(path, token string, body any, fault string) (int, http.Header, []byte) {
	c.t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		c.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, c.url+path, bytes.NewReader(b))
	if err != nil {
		c.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if fault != "" {
		req.Header.Set("X-Lmmock-Fault", fault)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer r.Body.Close()
	out, err := io.ReadAll(r.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return r.StatusCode, r.Header, out
}
func object(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var o map[string]any
	if err := json.Unmarshal(b, &o); err != nil {
		t.Fatalf("JSON: %s: %v", b, err)
	}
	return o
}
func (c cloudClient) call(name string, args map[string]any) map[string]any {
	c.t.Helper()
	status, _, b := c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "id": "demo-id", "method": "tools/call", "params": map[string]any{"name": name, "arguments": args}}, "")
	if status != 200 {
		c.t.Fatalf("%s: %d %s", name, status, b)
	}
	res := object(c.t, b)
	if res["id"] != "demo-id" {
		c.t.Fatalf("id not preserved: %s", b)
	}
	return res
}
func payload(t *testing.T, res map[string]any) any {
	t.Helper()
	r := res["result"].(map[string]any)
	if r["isError"] == true {
		t.Fatalf("tool failed: %v", res)
	}
	text := r["content"].([]any)[0].(map[string]any)["text"].(string)
	var out any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func (c cloudClient) tool(name string, args map[string]any) map[string]any {
	c.t.Helper()
	return payload(c.t, c.call(name, args)).(map[string]any)
}
func (c cloudClient) issue(key string) map[string]any {
	return c.tool("getJiraIssue", map[string]any{"issueIdOrKey": key})
}
func (c cloudClient) graphql(q string, vars map[string]any) map[string]any {
	c.t.Helper()
	status, _, b := c.request("/api/v2/graphql", "fixture-xray-token", map[string]any{"query": q, "variables": vars}, "")
	if status != 200 {
		c.t.Fatalf("graphql: %d %s", status, b)
	}
	return object(c.t, b)
}

func TestCloudMCPProtocolAndAuth(t *testing.T) {
	c := cloudServer(t, "")
	status, _, _ := c.request("/v1/mcp", "", map[string]any{"method": "initialize"}, "")
	if status != 401 {
		t.Fatal(status)
	}
	status, _, _ = c.request("/v1/mcp", "expired-token", map[string]any{"method": "initialize"}, "")
	if status != 401 {
		t.Fatal(status)
	}
	status, header, b := c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"}, "")
	if status != 200 || header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("initialize: %d %s", status, b)
	}
	result := object(t, b)["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatal(result)
	}
	status, _, b = c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"}, "")
	if status != 202 || len(b) != 0 {
		t.Fatalf("notification %d %s", status, b)
	}
	_, _, b = c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/list"}, "")
	tools := object(t, b)["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, v := range tools {
		o := v.(map[string]any)
		names[o["name"].(string)] = true
	}
	for _, name := range []string{"getJiraIssue", "getConfluencePage", "transitionJiraIssue", "lookupJiraAccountId"} {
		if !names[name] {
			t.Fatal(name)
		}
	}
	_, _, b = c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "id": 3, "method": "unknown"}, "")
	if object(t, b)["error"].(map[string]any)["code"] != float64(-32601) {
		t.Fatal(string(b))
	}
	if _, ok := c.call("unknown", nil)["error"]; !ok {
		t.Fatal("unknown tool accepted")
	}
}

func TestCloudJiraStateProjectionAndPagination(t *testing.T) {
	c := cloudServer(t, "")
	projected := c.tool("getJiraIssue", map[string]any{"issueIdOrKey": "10001", "fields": []string{"summary"}, "expand": "names"})
	if len(projected["fields"].(map[string]any)) != 1 || projected["expand"] != "names" || projected["names"].(map[string]any)["summary"] != "summary" {
		t.Fatal(projected)
	}
	r := c.tool("searchJiraIssuesUsingJql", map[string]any{"jql": "project = TEST ORDER BY key ASC", "maxResults": 1})
	if r["total"] != float64(4) || r["nextPageToken"] != "1" {
		t.Fatal(r)
	}
	second := c.tool("searchJiraIssuesUsingJql", map[string]any{"jql": "project = TEST ORDER BY key ASC", "maxResults": 1, "nextPageToken": r["nextPageToken"]})
	if second["issues"].([]any)[0].(map[string]any)["key"] != "TEST-2" {
		t.Fatal(second)
	}
	for _, q := range []string{"project = TEST AND status = \"To Do\"", "key = TEST-1", "text ~ \"synthetic\"", "ORDER BY key DESC"} {
		got := c.tool("searchJiraIssuesUsingJql", map[string]any{"jql": q})
		if len(got["issues"].([]any)) == 0 {
			t.Fatal(q, got)
		}
	}
	for _, q := range []string{"invalid", "project != TEST", "project = TEST OR key = TEST-1", "project =", "project = \"TEST", "text ~ unquoted", "project = TEST ORDER BY unknown"} {
		res := c.call("searchJiraIssuesUsingJql", map[string]any{"jql": q})
		if res["result"].(map[string]any)["isError"] != true {
			t.Fatalf("accepted %q", q)
		}
	}
	c.tool("addCommentToJiraIssue", map[string]any{"issueIdOrKey": "TEST-1", "commentBody": "Demo comment"})
	comments := c.issue("TEST-1")["fields"].(map[string]any)["comment"].(map[string]any)
	if comments["total"] != float64(1) || comments["comments"].([]any)[0].(map[string]any)["body"] != "Demo comment" {
		t.Fatal(comments)
	}
	available := c.tool("getTransitionsForJiraIssue", map[string]any{"issueIdOrKey": "TEST-1"})["transitions"].([]any)
	if len(available) != 1 {
		t.Fatal(available)
	}
	for _, transition := range []struct{ id, category, name string }{{"21", "indeterminate", "In Progress"}, {"31", "done", "Done"}} {
		c.tool("transitionJiraIssue", map[string]any{"issueIdOrKey": "TEST-1", "transition": map[string]any{"id": transition.id}})
		category := c.issue("TEST-1")["fields"].(map[string]any)["status"].(map[string]any)["statusCategory"].(map[string]any)
		if category["key"] != transition.category || category["name"] != transition.name {
			t.Fatal(category)
		}
	}
	if c.issue("TEST-1")["fields"].(map[string]any)["status"].(map[string]any)["name"] != "Done" {
		t.Fatal("status did not change")
	}
	if c.call("transitionJiraIssue", map[string]any{"issueIdOrKey": "TEST-1", "transition": map[string]any{"id": "21"}})["result"].(map[string]any)["isError"] != true {
		t.Fatal("invalid transition accepted")
	}
	c.tool("editJiraIssue", map[string]any{"issueIdOrKey": "TEST-1", "fields": map[string]any{"summary": "Edited demo", "labels": []string{"changed"}}})
	if c.issue("TEST-1")["fields"].(map[string]any)["summary"] != "Edited demo" {
		t.Fatal("edit did not persist")
	}
	created := c.tool("createJiraIssue", map[string]any{"projectKey": "TEST", "issueTypeName": "Task", "summary": "Created demo", "additional_fields": map[string]any{"labels": []string{"created"}}})
	category := created["fields"].(map[string]any)["status"].(map[string]any)["statusCategory"].(map[string]any)
	if category["key"] != "new" || category["name"] != "To Do" {
		t.Fatal(category)
	}
	key := created["key"].(string)
	if key != "TEST-5" || c.issue(key)["fields"].(map[string]any)["summary"] != "Created demo" {
		t.Fatal(created)
	}
	if c.tool("searchJiraIssuesUsingJql", map[string]any{"jql": "project = TEST"})["total"] != float64(5) {
		t.Fatal("new issue absent from search")
	}
	missing := c.call("getJiraIssue", map[string]any{"issueIdOrKey": "TEST-999"})
	if missing["result"].(map[string]any)["isError"] != true {
		t.Fatal(missing)
	}
	payload(t, c.call("atlassianUserInfo", nil))
	payload(t, c.call("lookupJiraAccountId", map[string]any{"searchString": "Test User"}))
	payload(t, c.call("getAccessibleAtlassianResources", nil))
	status, _, _ := c.request("/__reset", "fixture-token", nil, "")
	if status != 200 {
		t.Fatal(status)
	}
	if c.issue("TEST-1")["fields"].(map[string]any)["summary"] == "Edited demo" {
		t.Fatal("reset retained edits")
	}
	if c.call("getJiraIssue", map[string]any{"issueIdOrKey": "TEST-5"})["result"].(map[string]any)["isError"] != true {
		t.Fatal("reset retained created issue")
	}
}

func TestCloudConfluenceReadShapes(t *testing.T) {
	c := cloudServer(t, "")
	page := c.tool("getConfluencePage", map[string]any{"pageId": "20001", "contentFormat": "markdown"})
	if !strings.HasPrefix(page["body"].(string), "# Demo") {
		t.Fatal(page)
	}
	html := c.tool("getConfluencePage", map[string]any{"pageId": "20001", "contentFormat": "html"})
	if !strings.Contains(html["body"].(string), "<h1>") {
		t.Fatal(html)
	}
	for _, tc := range []struct {
		name  string
		args  map[string]any
		count int
	}{
		{"getConfluenceSpaces", nil, 1}, {"searchConfluenceUsingCql", map[string]any{"cql": "type = page"}, 2}, {"getPagesInConfluenceSpace", map[string]any{"spaceId": "20000"}, 2}, {"getConfluencePageDescendants", map[string]any{"pageId": "20001"}, 1}, {"getConfluencePageFooterComments", map[string]any{"pageId": "20001"}, 1}, {"getConfluencePageInlineComments", map[string]any{"pageId": "20001"}, 0}, {"getConfluenceCommentChildren", map[string]any{"commentId": "30001"}, 0},
	} {
		r := c.tool(tc.name, tc.args)
		if len(r["results"].([]any)) != tc.count {
			t.Fatal(tc.name, r)
		}
	}
	r := c.tool("getPagesInConfluenceSpace", map[string]any{"spaceId": "20000", "limit": 1})
	if r["_links"].(map[string]any)["next"] != "/pages?cursor=1" {
		t.Fatal(r)
	}
	r = c.tool("getPagesInConfluenceSpace", map[string]any{"spaceId": "20000", "limit": 1, "cursor": "1"})
	if r["results"].([]any)[0].(map[string]any)["id"] != "20002" {
		t.Fatal(r)
	}
}

func TestCloudXrayStateAndErrors(t *testing.T) {
	c := cloudServer(t, "")
	status, _, b := c.request("/api/v2/authenticate", "", map[string]any{"client_id": "fixture-client", "client_secret": "fixture-secret"}, "")
	if status != 200 || string(b) != "\"fixture-xray-token\"" {
		t.Fatalf("auth: %d %s", status, b)
	}
	status, _, _ = c.request("/api/v2/authenticate", "", map[string]any{"client_id": "wrong", "client_secret": "wrong"}, "")
	if status != 401 {
		t.Fatal(status)
	}
	status, _, _ = c.request("/api/v2/graphql", "expired-token", map[string]any{}, "")
	if status != 401 {
		t.Fatal(status)
	}
	testQuery := `query($id:String!){getTest(issueId:$id){issueId testType{name kind} steps{id action data result}}}`
	test := c.graphql(testQuery, map[string]any{"id": "10003"})["data"].(map[string]any)["getTest"].(map[string]any)
	if len(test["steps"].([]any)) != 1 {
		t.Fatal(test)
	}
	r := c.graphql(`mutation($id:String!,$s:CreateStepInput!){addTestStep(issueId:$id,step:$s){id action data result}}`, map[string]any{"id": "10003", "s": map[string]any{"action": "Demo action", "data": "Demo data", "result": "Demo result"}})
	if r["errors"] != nil {
		t.Fatal(r)
	}
	test = c.graphql(testQuery, map[string]any{"id": "10003"})["data"].(map[string]any)["getTest"].(map[string]any)
	if len(test["steps"].([]any)) != 2 {
		t.Fatal(test)
	}
	rid := "000000000000000000000001"
	for _, tc := range []struct {
		q string
		v map[string]any
	}{
		{`mutation($id:String!,$status:String!){updateTestRunStatus(id:$id,status:$status)}`, map[string]any{"id": rid, "status": "PASSED"}},
		{`mutation($id:String!,$comment:String!){updateTestRunComment(id:$id,comment:$comment)}`, map[string]any{"id": rid, "comment": "Demo run comment"}},
		{`mutation($id:String!,$ev:[AttachmentDataInput!]!){addEvidenceToTestRun(id:$id,evidence:$ev){warnings}}`, map[string]any{"id": rid, "ev": []any{map[string]any{"filename": "demo.txt", "mimeType": "text/plain", "data": "ZGVtbw=="}}}},
	} {
		if r := c.graphql(tc.q, tc.v); r["errors"] != nil {
			t.Fatal(r)
		}
	}
	runsQuery := `query($id:String!,$limit:Int!,$start:Int!){getTestExecution(issueId:$id){issueId testRuns(limit:$limit,start:$start){total results{id status{name} test{issueId jira(fields:["key","summary"])}}}}}`
	first := c.graphql(runsQuery, map[string]any{"id": "10004", "limit": 1, "start": 0})["data"].(map[string]any)["getTestExecution"].(map[string]any)["testRuns"].(map[string]any)
	run := first["results"].([]any)[0].(map[string]any)
	if first["total"] != float64(2) || run["status"].(map[string]any)["name"] != "PASSED" || run["comment"] != "Demo run comment" || len(run["evidence"].([]any)) != 1 {
		t.Fatal(first)
	}
	second := c.graphql(runsQuery, map[string]any{"id": "10004", "limit": 1, "start": 1})["data"].(map[string]any)["getTestExecution"].(map[string]any)["testRuns"].(map[string]any)
	if second["results"].([]any)[0].(map[string]any)["id"] == rid {
		t.Fatal("pagination repeated")
	}
	if c.graphql(testQuery, map[string]any{"id": "99999"})["data"].(map[string]any)["getTest"] != nil {
		t.Fatal("missing test should be null")
	}
	if c.graphql(`query{unsupported}`, nil)["errors"] == nil {
		t.Fatal("missing GraphQL error envelope")
	}
	c.request("/__reset", "fixture-token", nil, "")
	if len(c.graphql(testQuery, map[string]any{"id": "10003"})["data"].(map[string]any)["getTest"].(map[string]any)["steps"].([]any)) != 1 {
		t.Fatal("reset retained steps")
	}
}

func TestCloudOAuthPKCEAndRefresh(t *testing.T) {
	c := cloudServer(t, "")
	r, err := http.Get(c.url + "/.well-known/oauth-authorization-server")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if object(t, b)["token_endpoint"] != c.url+"/v1/token" {
		t.Fatal(string(b))
	}
	status, _, _ := c.request("/v1/register", "", map[string]any{"redirect_uris": []string{"http://127.0.0.1:12345/callback"}}, "")
	if status != 201 {
		t.Fatal(status)
	}
	verifier := "demo-pkce-verifier"
	hash := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(hash[:])
	redirect := "http://127.0.0.1:12345/callback"
	args := url.Values{"client_id": {"fixture-client"}, "redirect_uri": {redirect}, "state": {"demo-state"}, "code_challenge": {challenge}, "code_challenge_method": {"S256"}}
	noRedirect := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err = noRedirect.Get(c.url + "/v1/authorize?" + args.Encode())
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 302 {
		t.Fatal(r.StatusCode)
	}
	location, _ := url.Parse(r.Header.Get("Location"))
	if location.Query().Get("state") != "demo-state" {
		t.Fatal(location)
	}
	form := url.Values{"client_id": {"fixture-client"}, "redirect_uri": {redirect}, "code": {location.Query().Get("code")}, "code_verifier": {verifier}, "grant_type": {"authorization_code"}}
	for _, expected := range []int{200, 400} {
		r, err = http.PostForm(c.url+"/v1/token", form)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != expected {
			t.Fatalf("one-use code got %d want %d", r.StatusCode, expected)
		}
	}
	r, err = http.PostForm(c.url+"/v1/token", url.Values{"client_id": {"fixture-client"}, "grant_type": {"refresh_token"}, "refresh_token": {"fixture-refresh"}})
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
}

func TestCloudFaultsAndLatency(t *testing.T) {
	c := cloudServer(t, `{"failures":2,"failureStatus":429,"delayMs":5}`)
	for _, expected := range []int{429, 429, 200} {
		start := time.Now()
		status, h, _ := c.request("/v1/mcp", "fixture-token", map[string]any{"method": "initialize", "id": 1}, "")
		if status != expected {
			t.Fatalf("got %d want %d", status, expected)
		}
		if expected == 429 && h.Get("Retry-After") != "1" {
			t.Fatal(h)
		}
		if time.Since(start) < 5*time.Millisecond {
			t.Fatal("latency missing")
		}
	}
	for _, s := range []struct {
		fault  string
		status int
	}{{"403", 403}, {"429", 429}, {"500", 500}} {
		status, _, _ := c.request("/v1/mcp", "fixture-token", map[string]any{"method": "initialize", "id": 1}, s.fault)
		if status != s.status {
			t.Fatal(status)
		}
	}
}

func TestCloudMCPSSE(t *testing.T) {
	c := cloudServer(t, `{"mcpSse":true}`)
	for _, tool := range []string{"getJiraIssue", "unknown"} {
		args := map[string]any{"issueIdOrKey": "TEST-1"}
		status, h, b := c.request("/v1/mcp", "fixture-token", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]any{"name": tool, "arguments": args}}, "")
		if status != 200 {
			t.Fatal(status)
		}
		if tool == "unknown" {
			if object(t, b)["error"] == nil {
				t.Fatal(string(b))
			}
			continue
		}
		if h.Get("Content-Type") != "text/event-stream" || !strings.HasPrefix(string(b), "event: message\ndata: ") {
			t.Fatalf("SSE: %v %s", h, b)
		}
		body := strings.TrimSuffix(strings.TrimPrefix(string(b), "event: message\ndata: "), "\n\n")
		if payload(t, object(t, []byte(body))).(map[string]any)["key"] != "TEST-1" {
			t.Fatal(body)
		}
	}
}
