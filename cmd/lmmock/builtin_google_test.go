package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

func TestBuiltinGoogleAPIs(t *testing.T) {
	code, err := builtinProgram("google-apis")
	if err != nil {
		t.Fatalf("builtinProgram(google-apis): %v", err)
	}
	engine, err := mock.NewWithOptions(code, mock.Options{})
	if err != nil {
		t.Fatalf("mock.NewWithOptions: %v", err)
	}
	srv := httptest.NewServer(engine)
	defer srv.Close()

	doReq := func(method, path, ct, body, token string) (int, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: %v", method, path, err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}

	if st, _ := doReq("GET", "/v1beta/accounts", "", "", "wrong"); st != 401 {
		t.Fatalf("expected 401 on bad token, got %d", st)
	}

	st, body := doReq("GET", "/v1beta/accountSummaries", "", "", "fixture-token")
	if st != 200 || !strings.Contains(body, "properties/3001001") {
		t.Fatalf("accountSummaries = %d %s", st, body)
	}

	var rep struct {
		RowCount int `json:"rowCount"`
	}
	st, body = doReq("POST", "/v1beta/properties/3001001:runReport", "application/json",
		`{"dimensions":[{"name":"country"}],"metrics":[{"name":"activeUsers"}],"dimensionFilter":{"filter":{"fieldName":"country","stringFilter":{"value":"United States"}}},"limit":"10"}`,
		"fixture-token")
	if st != 200 || json.Unmarshal([]byte(body), &rep) != nil || rep.RowCount != 2 {
		t.Fatalf("runReport = %d %s", st, body)
	}

	multipart := bytes.NewBufferString("--b1\r\nContent-Type: application/json\r\n\r\n{\"snippet\":{\"title\":\"Builtin Upload\",\"description\":\"ok\"},\"status\":{\"privacyStatus\":\"private\"}}\r\n--b1\r\nContent-Type: application/octet-stream\r\n\r\nrawbytes\r\n--b1--\r\n")
	st, body = doReq("POST", "/upload/youtube/v3/videos?uploadType=multipart&part=snippet,status",
		"multipart/related; boundary=b1", multipart.String(), "fixture-token")
	if st != 200 || !strings.Contains(body, "Builtin Upload") {
		t.Fatalf("youtube upload = %d %s", st, body)
	}

	st, body = doReq("GET", "/v2/reports?ids=channel%3D%3DMINE&startDate=2026-11-01&endDate=2026-11-15&dimensions=day%2Cvideo&metrics=views&filters=video%3D%3Dvid_launch_04",
		"", "", "fixture-token")
	if st != 200 || !strings.Contains(body, "vid_launch_04") {
		t.Fatalf("youtube analytics = %d %s", st, body)
	}

	st, body = doReq("GET", "/gmail/v1/users/me/messages?q=is%3Aunread", "", "", "fixture-token")
	if st != 200 || !strings.Contains(body, "msg_1001") {
		t.Fatalf("gmail search = %d %s", st, body)
	}
	st, body = doReq("GET", "/gmail/v1/users/me/messages/msg_1001?format=full", "", "", "fixture-token")
	if st != 200 || !strings.Contains(body, "thr_1001") {
		t.Fatalf("gmail get = %d %s", st, body)
	}
}
