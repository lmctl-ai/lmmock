package mock

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func proxyEngine(t *testing.T, options Options) *Engine {
	t.Helper()
	e, err := NewWithOptions(fixture(t, "api"), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.forwarder.client.CloseIdleConnections)
	return e
}

func TestProxyMockPrecedenceAndAudit(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("X-Upstream", "yes")
		w.WriteHeader(418)
		_, _ = w.Write([]byte("upstream\x00bytes"))
	}))
	defer up.Close()
	var audit bytes.Buffer
	e := proxyEngine(t, Options{Upstream: up.URL, AuditWriter: &audit})
	for _, path := range []string{"/health", "/unmatched"} {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if path == "/health" {
			if w.Code != 200 || calls.Load() != 0 || w.Header().Get("X-Upstream") != "" {
				t.Fatalf("mock lost precedence: %v", w)
			}
		} else if w.Code != 418 || w.Header().Get("X-Upstream") != "yes" || w.Body.String() != "upstream\x00bytes" {
			t.Fatalf("response changed: %v", w)
		}
	}
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(audit.String()), "\n") {
		var e map[string]any
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	if len(events) != 2 || events[0]["kind"] != "mocked" || events[1]["kind"] != "forwarded" || calls.Load() != 1 {
		t.Fatalf("bad audit: %s", audit.String())
	}
}

func TestProxyPreservesRequestAndStripsHopHeaders(t *testing.T) {
	body := strings.Repeat("binary\x00", 200000) // Larger than the mock handler's 1 MiB body limit.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if r.Method != "PATCH" || r.URL.RequestURI() != "/repository/public/a%2Fb//c?x=1&x=2&q=a%2Bb" || string(b) != body {
			t.Errorf("request changed: %s %s %d", r.Method, r.URL.RequestURI(), len(b))
		}
		if r.Host != strings.TrimPrefix(r.Context().Value(http.LocalAddrContextKey).(net.Addr).String(), "http://") {
			t.Errorf("wrong upstream authority: %s", r.Host)
		}
		for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Authorization", "Proxy-Test", "TE", "Trailer", "Upgrade", "X-Hop"} {
			if v := r.Header.Get(name); v != "" {
				t.Errorf("leaked %s: %s", name, v)
			}
		}
		if r.Header.Get("Authorization") != "Bearer test" || r.Header.Get("X-End-To-End") != "preserved" || len(r.Header.Values("X-Multi")) != 2 {
			t.Errorf("lost end-to-end headers: %v", r.Header)
		}
		w.Header().Set("Connection", "X-Response-Hop, keep-alive")
		w.Header().Set("X-Response-Hop", "remove")
		w.Header().Set("Keep-Alive", "timeout=9")
		w.Header().Set("Proxy-Test", "remove")
		w.Header().Set("TE", "trailers")
		w.Header().Set("Trailer", "X-Trailer")
		w.Header().Set("Upgrade", "websocket")
		w.Header().Add("X-Multi", "one")
		w.Header().Add("X-Multi", "two")
		w.WriteHeader(201)
		_, _ = w.Write([]byte("returned\x00bytes"))
	}))
	defer up.Close()
	e := proxyEngine(t, Options{Upstream: up.URL + "/repository/public/"})
	r := httptest.NewRequest("PATCH", "/a%2Fb//c?x=1&x=2&q=a%2Bb", strings.NewReader(body))
	r.Header = http.Header{"Connection": {"X-Hop, keep-alive"}, "X-Hop": {"remove"}, "Keep-Alive": {"timeout=4"}, "Proxy-Authorization": {"secret"}, "Proxy-Test": {"remove"}, "Te": {"trailers"}, "Trailer": {"X-Trailer"}, "Upgrade": {"websocket"}, "Authorization": {"Bearer test"}, "X-End-To-End": {"preserved"}, "X-Multi": {"one", "two"}}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, r)
	if w.Code != 201 || w.Body.String() != "returned\x00bytes" || len(w.Header().Values("X-Multi")) != 2 {
		t.Fatalf("bad relay: %v", w)
	}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Test", "TE", "Trailer", "Upgrade", "X-Response-Hop"} {
		if v := w.Header().Get(name); v != "" {
			t.Errorf("response leaked %s: %s", name, v)
		}
	}
}

func TestProxyHEADStatusesRedirectAndEncoding(t *testing.T) {
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, _ = z.Write([]byte("encoded body"))
	_ = z.Close()
	var redirects atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/head":
			w.Header().Set("Content-Length", "12345")
			w.Header().Set("X-Test", "head")
			w.WriteHeader(200)
		case "/redirect":
			w.Header().Set("Location", "/destination")
			w.WriteHeader(307)
			_, _ = w.Write([]byte("redirect body"))
		case "/destination":
			redirects.Add(1)
			w.WriteHeader(200)
		case "/gzip":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(compressed.Bytes())
		default:
			w.Header().Set("X-Test", "missing")
			w.WriteHeader(404)
			_, _ = w.Write([]byte("missing\x00body"))
		}
	}))
	defer up.Close()
	s := httptest.NewServer(proxyEngine(t, Options{Upstream: up.URL}))
	defer s.Close()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: &http.Transport{DisableCompression: true}}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, path, body string
		status             int
	}{{"HEAD", "/head", "", 200}, {"GET", "/redirect", "redirect body", 307}, {"GET", "/missing", "missing\x00body", 404}, {"GET", "/gzip", compressed.String(), 200}} {
		req, _ := http.NewRequest(tc.method, s.URL+tc.path, nil)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != tc.status || string(b) != tc.body {
			t.Fatalf("%s: %d %q", tc.path, resp.StatusCode, b)
		}
		if tc.method == "HEAD" && resp.Header.Get("Content-Length") != "12345" {
			t.Fatal("HEAD length lost")
		}
		if tc.path == "/redirect" && resp.Header.Get("Location") != "/destination" {
			t.Fatal("redirect changed")
		}
		if tc.path == "/gzip" && resp.Header.Get("Content-Encoding") != "gzip" {
			t.Fatal("decompressed response")
		}
	}
	if redirects.Load() != 0 {
		t.Fatal("followed redirect")
	}
}

func TestProxyFailuresAndReadOnly(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	unreachable := "http://" + l.Addr().String()
	_ = l.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer slow.Close()
	for _, tc := range []struct {
		upstream string
		status   int
	}{{unreachable, 502}, {slow.URL, 504}} {
		e := proxyEngine(t, Options{Upstream: tc.upstream, UpstreamTimeout: 50 * time.Millisecond})
		w := httptest.NewRecorder()
		start := time.Now()
		e.ServeHTTP(w, httptest.NewRequest("GET", "/missing", nil))
		if w.Code != tc.status || !strings.Contains(w.Body.String(), "upstream request failed") || time.Since(start) > time.Second {
			t.Fatalf("failure not prompt/clear: %v", w)
		}
	}
	e := proxyEngine(t, Options{Upstream: unreachable, UpstreamReadOnly: true})
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("POST", "/missing", nil))
	if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
		t.Fatalf("write forwarded: %v", w)
	}
	for _, upstream := range []string{"ftp://example.com", "/relative", "http://user:pass@example.com", "http://example.com/?secret=x", "http://example.com/#fragment"} {
		if _, err := NewWithOptions(fixture(t, "api"), Options{Upstream: upstream}); err == nil {
			t.Errorf("accepted %s", upstream)
		}
	}
}

func TestProxyHTTPS(t *testing.T) {
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("tls upstream")) }))
	defer up.Close()
	e := proxyEngine(t, Options{Upstream: up.URL})
	// Trust only this test certificate; production uses Go's normal system roots.
	pool := x509.NewCertPool()
	pool.AddCert(up.Certificate())
	e.forwarder.client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/unmatched", nil))
	if w.Code != 200 || w.Body.String() != "tls upstream" {
		t.Fatalf("TLS failed: %v", w)
	}
}

func TestProxyAbortsTimedOutResponseBody(t *testing.T) {
	chunk := strings.Repeat("x", 32<<10) // Exceed net/http's small write buffer before the abort.
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte(chunk))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer up.Close()
	s := httptest.NewServer(proxyEngine(t, Options{Upstream: up.URL, UpstreamTimeout: 100 * time.Millisecond}))
	defer s.Close()
	start := time.Now()
	resp, err := http.Get(s.URL + "/partial")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(b) != chunk || err == nil || time.Since(start) > time.Second {
		t.Fatalf("partial response should abort promptly without changing status/body: %d %q %v", resp.StatusCode, b, err)
	}
}

func TestProxyStreamsLargeResponse(t *testing.T) {
	const size = 200 << 20
	first, release := make(chan struct{}), make(chan struct{})
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 32<<10)
		_, _ = w.Write(buf)
		w.(http.Flusher).Flush()
		close(first)
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		for n := len(buf); n < size; n += len(buf) {
			if _, err := w.Write(buf); err != nil {
				return
			}
		}
	}))
	defer up.Close()
	s := httptest.NewServer(proxyEngine(t, Options{Upstream: up.URL}))
	defer s.Close()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	resp, err := http.Get(s.URL + "/large")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	<-first
	buf := make([]byte, 16<<10)
	if _, err := io.ReadFull(resp.Body, buf); err != nil {
		t.Fatal(err)
	}
	close(release) // Client received body bytes before upstream finished producing it.
	n, err := io.Copy(io.Discard, resp.Body)
	if err != nil || n+int64(len(buf)) != size {
		t.Fatalf("truncated: %d %v", n, err)
	}
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("streamed %d bytes; in-process total allocated %d bytes", size, allocated)
	if allocated > 32<<20 {
		t.Fatalf("response appears buffered: allocated %d", allocated)
	}
}
