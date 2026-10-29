package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// A single fixed upstream, not an HTTP CONNECT or arbitrary destination proxy.
type forwarder struct {
	base     *url.URL
	client   *http.Client
	readOnly bool
}

func newForwarder(options Options) (*forwarder, error) {
	u, err := url.Parse(options.Upstream)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") ||
		u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("upstream must be an absolute http(s) URL without credentials, query or fragment")
	}
	timeout := options.UpstreamTimeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	if timeout < 0 {
		return nil, errors.New("upstream timeout must be positive")
	}
	transport := &http.Transport{
		// No environment proxy, cookie jar, authentication or automatic decompression.
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: min(15*time.Second, timeout),
		IdleConnTimeout:       30 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 8,
		DisableCompression: true,
	}
	return &forwarder{base: u, readOnly: options.UpstreamReadOnly, client: &http.Client{
		Transport: transport, Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func stripHopHeaders(h http.Header) {
	for _, connection := range h.Values("Connection") {
		for _, name := range strings.Split(connection, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for name := range h {
		if strings.HasPrefix(strings.ToLower(name), "proxy-") {
			h.Del(name)
		}
	}
	for _, name := range []string{"Connection", "Keep-Alive", "TE", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(name)
	}
}

func (f *forwarder) serve(w http.ResponseWriter, r *http.Request) (string, string, bool) {
	if r.URL.Path == "*" {
		http.Error(w, "asterisk request target cannot be forwarded", http.StatusBadRequest)
		return "", "asterisk request target cannot be forwarded", false
	}
	// Path is already URL-decoded, including encoded slashes and dot segments.
	for _, segment := range strings.Split(r.URL.Path, "/") {
		if segment == "." || segment == ".." {
			http.Error(w, "path must not contain dot segments", http.StatusBadRequest)
			return "", "path must not contain dot segments", false
		}
	}
	if f.readOnly && r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "upstream is read-only: only GET and HEAD may be forwarded", http.StatusMethodNotAllowed)
		return "", "read-only upstream rejected method", false
	}
	u := *f.base
	// Append the original escaped path without cleaning or decoding its spelling.
	u.RawPath = strings.TrimSuffix(f.base.EscapedPath(), "/") + r.URL.EscapedPath()
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.RawQuery, u.ForceQuery = r.URL.RawQuery, r.URL.ForceQuery
	out := r.Clone(r.Context())
	out.URL, out.RequestURI, out.Host = &u, "", u.Host
	stripHopHeaders(out.Header)
	if f.readOnly {
		for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
			out.Header.Del(name)
		}
	}
	out.TransferEncoding, out.Trailer, out.Close = nil, nil, false
	if _, ok := out.Header["User-Agent"]; !ok {
		out.Header["User-Agent"] = []string{""}
	}
	resp, err := f.client.Do(out)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return u.String(), "client closed request", false
		}
		code := http.StatusBadGateway
		var timeout net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
			code = http.StatusGatewayTimeout
		}
		message := fmt.Sprintf("upstream request failed: %v", err)
		http.Error(w, message, code)
		return u.String(), message, false
	}
	defer resp.Body.Close()
	stripHopHeaders(resp.Header)
	for name, values := range resp.Header {
		w.Header()[name] = values
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return u.String(), "", false
	}
	// Flush headers and stream with bounded memory; never materialize artifact bodies in the VM.
	_ = http.NewResponseController(w).Flush()
	if _, err = io.Copy(w, resp.Body); err != nil {
		// Headers are already committed: abort rather than append a fake error body/status.
		if errors.Is(r.Context().Err(), context.Canceled) {
			return u.String(), "client closed request", true
		}
		return u.String(), "upstream response stream failed: " + err.Error(), true
	}
	return u.String(), "", false
}

type auditResponse struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *auditResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *auditResponse) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *auditResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (e *Engine) audit(r *http.Request, w *auditResponse, kind, upstream, failure string, start time.Time) {
	if e.options.AuditWriter == nil {
		return
	}
	headers := r.Header.Clone()
	for _, name := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if headers.Get(name) != "" {
			headers.Set(name, "[redacted]")
		}
	}
	e.auditMu.Lock()
	defer e.auditMu.Unlock()
	_ = json.NewEncoder(e.options.AuditWriter).Encode(struct {
		Time       string      `json:"time"`
		Kind       string      `json:"kind"`
		Method     string      `json:"method"`
		URL        string      `json:"url"`
		Host       string      `json:"host"`
		Headers    http.Header `json:"headers"`
		Status     int         `json:"status"`
		Bytes      int64       `json:"bytes"`
		DurationMS int64       `json:"duration_ms"`
		Upstream   string      `json:"upstream,omitempty"`
		Error      string      `json:"error,omitempty"`
	}{start.UTC().Format(time.RFC3339Nano), kind, r.Method, r.URL.RequestURI(), r.Host,
		headers, w.status, w.bytes, time.Since(start).Milliseconds(), upstream, failure})
}
