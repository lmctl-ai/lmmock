package mock

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"sync"

	lmcode "github.com/lmctl-ai/lmcode-go"
	"github.com/lmctl-ai/lmmock/internal/scenario"
)

type requestRecord struct {
	ID            uint64      `json:"id"`
	Method        string      `json:"method"`
	URI           string      `json:"uri"`
	Headers       http.Header `json:"headers"`
	Body          *string     `json:"body,omitempty"`
	BodyComplete  *bool       `json:"body_complete,omitempty"`
	BodyTruncated bool        `json:"body_truncated,omitempty"`
	Status        int         `json:"status"`
	Bytes         int64       `json:"bytes"`
	Error         string      `json:"error,omitempty"`
	Complete      bool        `json:"complete"`
}
type requestHistory struct {
	sync.Mutex
	records     []requestRecord
	next, epoch uint64
	bodies      bool
}

func (h *requestHistory) snapshot() string {
	if h == nil {
		return `{"enabled":false,"requests":[]}`
	}
	h.Lock()
	defer h.Unlock()
	records := append([]requestRecord{}, h.records...)
	sort.Slice(records, func(i, j int) bool { return records[i].ID < records[j].ID })
	raw, _ := json.Marshal(struct {
		Enabled  bool            `json:"enabled"`
		Requests []requestRecord `json:"requests"`
	}{true, records})
	return string(raw)
}
func (h *requestHistory) reset() {
	if h == nil {
		return
	}
	h.Lock()
	defer h.Unlock()
	h.records = nil
	h.next = 0
	h.epoch++
}

type loggedBody struct {
	sync.Mutex
	io.ReadCloser
	bytes.Buffer
	truncated bool
	eof       bool
}

func (b *loggedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.Lock()
	defer b.Unlock()
	if err == io.EOF {
		b.eof = true
	}
	keep := n
	if keep > 1<<20-b.Buffer.Len() {
		keep = 1<<20 - b.Buffer.Len()
		b.truncated = true
	}
	b.Buffer.Write(p[:keep])
	return n, err
}

type historyResponse struct {
	http.ResponseWriter
	status  int
	bytes   int64
	failure string
	changed func()
}

func (w *historyResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *historyResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
		if w.changed != nil {
			w.changed()
		}
	}
}
func (w *historyResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(p)
	w.bytes += int64(n)
	if err != nil {
		w.failure = "response write failed"
	}
	if w.changed != nil {
		w.changed()
	}
	return n, err
}
func (w *historyResponse) streamFailure(reason string) {
	w.failure = reason
	if w.changed != nil {
		w.changed()
	}
}
func markStreamFailure(w http.ResponseWriter, reason string) {
	for {
		if recorder, ok := w.(interface{ streamFailure(string) }); ok {
			recorder.streamFailure(reason)
			return
		}
		wrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		w = wrapper.Unwrap()
	}
}
func (e *Engine) beginRequestLog(w http.ResponseWriter, r *http.Request) (http.ResponseWriter, func()) {
	history := e.state.history
	if history == nil || r.URL.Path == "/__requests" || r.URL.Path == "/__reset" {
		return w, func() {}
	}
	history.Lock()
	history.next++
	id, epoch, index := history.next, history.epoch, len(history.records)
	history.records = append(history.records, requestRecord{})
	history.Unlock()
	capture := &loggedBody{ReadCloser: r.Body}
	r.Body = capture
	response := &historyResponse{ResponseWriter: w}
	complete := false
	update := func() {
		capture.Lock()
		raw := append([]byte(nil), capture.Buffer.Bytes()...)
		truncated := capture.truncated
		eof := capture.eof
		capture.Unlock()
		clean := scenario.RedactRequest(scenario.Request{Method: r.Method, Path: r.URL.RequestURI(), Headers: r.Header, Content: raw})
		record := requestRecord{ID: id, Method: r.Method, URI: clean.Path, Headers: http.Header(clean.Headers), Status: response.status, Bytes: response.bytes, Error: response.failure, Complete: complete}
		if history.bodies {
			body := string(clean.Content)
			record.Body = &body
			record.BodyTruncated = truncated
			record.BodyComplete = &eof
		}
		history.Lock()
		if epoch == history.epoch {
			history.records[index] = record
		}
		history.Unlock()
	}
	response.changed = update
	update()
	return response, func() {
		failure := recover()
		if failure != nil {
			response.failure = "response aborted"
		}
		complete = true
		update()
		if failure != nil {
			panic(failure)
		}
	}
}
func observabilityMethods(method exchangeMethod) {
	method("request_log", nil, "string", func(x *exchange, _ []lmcode.Value) (lmcode.Value, error) {
		return lmcode.String(x.state.history.snapshot()), nil
	})
	method("reset", nil, "void", func(x *exchange, _ []lmcode.Value) (lmcode.Value, error) {
		x.state.Lock()
		x.state.values = map[string]string{}
		x.state.counts = map[string]int64{}
		x.state.history.reset()
		x.state.Unlock()
		return lmcode.None(), nil
	})
}

// Reserved paths are ordinary lmcode routes. The Go layer only stores transport
// observations and shared state; it never chooses a service response or route.
