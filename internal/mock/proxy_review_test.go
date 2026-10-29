package mock

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyRejectsDotSegments(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer up.Close()
	var audit bytes.Buffer
	e := proxyEngine(t, Options{Upstream: up.URL + "/repository/public/", AuditWriter: &audit})
	for _, path := range []string{"/../../service/rest/v1/status", "/a/../../../b", "/%2e%2e/%2e%2e/c", "/./unmatched", "/a%2f..%2f..%2fb"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			e.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 400 || !strings.Contains(w.Body.String(), "path must not contain dot segments") {
				t.Fatalf("unsafe path accepted: %d %s", w.Code, w.Body.String())
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("dot-segment request reached upstream")
	}
	for _, line := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["kind"] != "forwarded" || event["status"] != float64(400) || event["error"] != "path must not contain dot segments" {
			t.Fatalf("bad rejection audit: %s", line)
		}
	}
}

func TestProxyMockedPathRetainsMethodNotAllowed(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(418) }))
	defer up.Close()
	e := proxyEngine(t, Options{Upstream: up.URL})
	for _, method := range []string{"POST", "DELETE"} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(method, "/health", nil))
		if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatalf("%s escaped mock: %d %v", method, w.Code, w.Header())
		}
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("HEAD", "/health", nil))
	if w.Code != 200 || calls.Load() != 0 {
		t.Fatal("mocked path reached upstream")
	}
}

func TestProxyReadOnlyStripsCredentials(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(readOnly), func(t *testing.T) {
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if readOnly {
					for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
						if r.Header.Get(name) != "" {
							t.Errorf("read-only leaked %s", name)
						}
					}
				} else if r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Cookie") != "session=test" {
					t.Error("normal forwarding lost end-to-end credentials")
				}
				if r.Header.Get("X-Test") != "preserved" {
					t.Error("lost unrelated header")
				}
			}))
			defer up.Close()
			e := proxyEngine(t, Options{Upstream: up.URL, UpstreamReadOnly: readOnly})
			r := httptest.NewRequest("GET", "/unmatched", nil)
			r.Header.Set("Authorization", "Bearer test")
			r.Header.Set("Proxy-Authorization", "Basic test")
			r.Header.Set("Cookie", "session=test")
			r.Header.Set("X-Test", "preserved")
			w := httptest.NewRecorder()
			e.ServeHTTP(w, r)
			if w.Code != 200 || r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("Cookie") != "session=test" {
				t.Fatal("request failed or original headers mutated")
			}
		})
	}
}

func TestMockBodyLimitClosesConnection(t *testing.T) {
	for _, audited := range []bool{false, true} {
		t.Run(fmt.Sprint(audited), func(t *testing.T) {
			options := Options{}
			if audited {
				options.AuditWriter = io.Discard
			}
			e, err := NewWithOptions(fixture(t, "api"), options)
			if err != nil {
				t.Fatal(err)
			}
			s := httptest.NewServer(e)
			defer s.Close()
			conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
			body := strings.Repeat("x", (1<<20)+(64<<10))
			if _, err = fmt.Fprintf(conn, "POST /echo HTTP/1.1\r\nHost: test\r\nContent-Length: %d\r\n\r\n%s", len(body), body); err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			resp, err := http.ReadResponse(reader, nil)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 413 || !resp.Close || err != nil || string(b) != "request body exceeds 1 MiB or cannot be read\n" {
				t.Fatalf("unclean limit response: %d close=%t body=%q err=%v", resp.StatusCode, resp.Close, b, err)
			}
			if _, err = reader.ReadByte(); err != io.EOF {
				t.Fatalf("want clean connection close, got %v", err)
			}
		})
	}
}

func TestProxyMock404AndFaultsDoNotFallThrough(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(418) }))
	defer up.Close()
	for _, name := range []string{"not-found", "faults"} {
		e, err := NewWithOptions(fixture(t, name), Options{Upstream: up.URL, AuditWriter: io.Discard})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(e.forwarder.client.CloseIdleConnections)
		s := httptest.NewServer(e)
		client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		if name == "not-found" {
			resp, err := client.Get(s.URL + "/missing")
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 404 || string(body) != "mocked missing" {
				t.Fatal("handler's 404 fell through")
			}
		} else {
			for _, status := range []int{503, 503, 200} {
				resp, err := client.Get(s.URL + "/retry")
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode != status {
					t.Fatalf("mock retry status %d, want %d", resp.StatusCode, status)
				}
			}
			start := time.Now()
			resp, err := client.Get(s.URL + "/slow")
			if err != nil {
				t.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != 200 || time.Since(start) < 450*time.Millisecond {
				t.Fatal("mock delay bypassed")
			}
			resp, err = client.Get(s.URL + "/disconnect")
			if resp != nil {
				_ = resp.Body.Close()
			}
			if err == nil {
				t.Fatal("mock disconnect bypassed")
			}
		}
		client.CloseIdleConnections()
		s.Close()
	}
	if calls.Load() != 0 {
		t.Fatal("explicit mock/fault contacted upstream")
	}
}

func TestProxyClientDisconnectCancelsUpstream(t *testing.T) {
	for _, midstream := range []bool{false, true} {
		t.Run(fmt.Sprint(midstream), func(t *testing.T) {
			started, canceled, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if midstream {
					_, _ = w.Write([]byte(strings.Repeat("x", 32<<10)))
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
				close(canceled)
			}))
			defer up.Close()
			var audit bytes.Buffer
			e := proxyEngine(t, Options{Upstream: up.URL, UpstreamTimeout: 3 * time.Second, AuditWriter: &audit})
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { defer close(done); e.ServeHTTP(w, r) }))
			defer s.Close()
			conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			_, _ = fmt.Fprint(conn, "GET /unmatched HTTP/1.1\r\nHost: test\r\n\r\n")
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("upstream did not start")
			}
			if midstream {
				resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := io.ReadFull(resp.Body, make([]byte, 16)); err != nil {
					t.Fatal(err)
				}
			}
			_ = conn.Close()
			select {
			case <-canceled:
			case <-time.After(2 * time.Second):
				t.Fatal("client disconnect did not cancel upstream")
			}
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("proxy did not finish")
			}
			var event map[string]any
			if err := json.Unmarshal(audit.Bytes(), &event); err != nil {
				t.Fatal(err)
			}
			if event["error"] != "client closed request" {
				t.Fatalf("misreported client cancellation: %s", audit.String())
			}
		})
	}
}

func TestProxyChunkedRequest(t *testing.T) {
	body := "chunked\x00request"
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil || string(b) != body || r.ContentLength != -1 || len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked" {
			t.Errorf("chunked request changed: %q len=%d encoding=%v err=%v", b, r.ContentLength, r.TransferEncoding, err)
		}
		w.WriteHeader(202)
		_, _ = w.Write(b)
	}))
	defer up.Close()
	s := httptest.NewServer(proxyEngine(t, Options{Upstream: up.URL}))
	defer s.Close()
	req, err := http.NewRequest("POST", s.URL+"/upload", io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != 202 || string(b) != body {
		t.Fatalf("bad chunked relay: %d %q %v", resp.StatusCode, b, err)
	}
}

func TestProxyRejectsAsteriskTarget(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer up.Close()
	s, err := Start(proxyEngine(t, Options{Upstream: up.URL}), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(s.URL, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = fmt.Fprint(conn, "OPTIONS * HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 400 || calls.Load() != 0 {
		t.Fatal("asterisk request was not rejected before upstream")
	}
}
