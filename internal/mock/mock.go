// Package mock binds verified lmcode entries to HTTP routes.
package mock

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	lmcode "github.com/lmctl-ai/lmcode-go"
)

//go:embed schema.json
var Schema []byte

// Previous schema remains accepted for already compiled fixtures.
//
//go:embed schema-v1.json
var legacySchema []byte

//go:embed schema-v3.json
var schemaV3 []byte

//go:embed schema-v4.json
var schemaV4 []byte

type route struct {
	method, path, annotation string
	entry                    int
}
type store struct {
	sync.Mutex
	values  map[string]string
	counts  map[string]int64
	queues  map[string][]string
	history *requestHistory
}

func (s *store) get(key string) string {
	s.Lock()
	defer s.Unlock()
	if s.values != nil {
		if v, ok := s.values[key]; ok {
			return v
		}
	}
	if s.counts != nil {
		if n, ok := s.counts[key]; ok && n != 0 {
			return strconv.FormatInt(n, 10)
		}
	}
	return ""
}

func (s *store) increment(key string) int64 {
	s.Lock()
	defer s.Unlock()
	if s.counts == nil {
		s.counts = map[string]int64{}
	}
	if s.values == nil {
		s.values = map[string]string{}
	}
	s.counts[key]++
	n := s.counts[key]
	s.values[key] = strconv.FormatInt(n, 10)
	return n
}

type Engine struct {
	result    *lmcode.ResultSink
	options   Options
	code      []byte
	schema    []byte
	routes    []route
	state     *store
	mux       *http.ServeMux
	forwarder *forwarder
	auditMu   sync.Mutex
}
type exchange struct {
	request       *http.Request
	body          string
	status        int
	headers       http.Header
	output        string
	stream        *responseStream
	delay         time.Duration
	drop          bool
	responseQueue string
	state         *store
}

func (x *exchange) lookupHeader(name string) string {
	if name == ":scheme" {
		if x.request != nil && x.request.TLS != nil {
			return "https"
		}
		return "http"
	}
	if name == ":body_json_kind" {
		trimmed := strings.TrimSpace(x.body)
		if trimmed == "" || !json.Valid([]byte(trimmed)) {
			return "invalid"
		}
		switch trimmed[0] {
		case '{':
			return "object"
		case '[':
			return "array"
		default:
			return "primitive"
		}
	}
	if strings.EqualFold(name, "Mcp-Name-Decoded") {
		if x.request == nil {
			return "\x00missing"
		}
		vals := x.request.Header.Values("Mcp-Name")
		if len(vals) == 0 {
			return "\x00missing"
		}
		if len(vals) > 1 {
			return "\x00invalid"
		}
		raw := vals[0]
		if encoded, ok := strings.CutPrefix(raw, "=?base64?"); ok {
			if encoded, ok = strings.CutSuffix(encoded, "?="); ok {
				decoded, err := base64.StdEncoding.DecodeString(encoded)
				if err != nil {
					return "\x00invalid"
				}
				return string(decoded)
			}
		}
		return raw
	}
	if x.request == nil {
		return ""
	}
	// net/http promotes Host out of Header when parsing an incoming request.
	if strings.EqualFold(name, "Host") {
		return x.request.Host
	}
	return x.request.Header.Get(name)
}

func enqueueResponse(x *exchange, payload string) {
	x.state.Lock()
	defer x.state.Unlock()
	if x.state.queues == nil {
		x.state.queues = map[string][]string{}
	}
	x.state.queues[x.responseQueue] = append(x.state.queues[x.responseQueue], payload)
}

func dequeueResponse(state *store, session string) string {
	state.Lock()
	defer state.Unlock()
	q := state.queues[session]
	if len(q) == 0 {
		return ""
	}
	value := q[0]
	if len(q) == 1 {
		delete(state.queues, session)
	} else {
		state.queues[session] = q[1:]
	}
	return value
}

func pauseContext(ctx context.Context, millis int64) (bool, error) {
	if millis < 0 || millis > int64(^uint64(0)>>1)/int64(time.Millisecond) {
		return false, errors.New("pause duration must be a non-negative representable millisecond count")
	}
	if ctx.Err() != nil {
		return false, nil
	}
	timer := time.NewTimer(time.Duration(millis) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true, nil
	case <-ctx.Done():
		return false, nil
	}
}

// New verifies all entries and route bindings before opening any network listener.
func New(code []byte) (*Engine, error) { return NewWithOptions(code, Options{}) }

func NewWithOptions(code []byte, options Options) (*Engine, error) {
	return NewWithInvocation(code, options, nil)
}

// NewWithInvocation verifies and registers the program in its run-owned result collector.
func NewWithInvocation(code []byte, options Options, results *lmcode.InvocationResults) (*Engine, error) {
	e := &Engine{options: options, code: append([]byte(nil), code...), state: &store{values: map[string]string{}, counts: map[string]int64{}}, mux: http.NewServeMux()}
	if options.RequestLog || options.RequestLogBodies {
		e.state.history = &requestHistory{bodies: options.RequestLogBodies}
	}
	var err error
	if options.Upstream != "" {
		e.forwarder, err = newForwarder(options)
		if err != nil {
			return nil, err
		}
	}
	reg := registry(e.options, e.state, func(method, path, annotation string) error {
		if !strings.Contains(" GET POST PUT PATCH DELETE HEAD OPTIONS ", " "+method+" ") || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \t\r\n?#") {
			return fmt.Errorf("invalid route %q %q", method, path)
		}
		for _, r := range e.routes {
			if r.method == method && r.path == path {
				return fmt.Errorf("duplicate route %s %s", method, path)
			}
		}
		e.routes = append(e.routes, route{method: method, path: path, annotation: annotation})
		return nil
	})
	e.schema = Schema
	p, err := lmcode.Load(e.code, e.schema, reg)
	if err != nil {
		for _, schema := range [][]byte{schemaV4, schemaV3, legacySchema} {
			if old, oldErr := lmcode.Load(e.code, schema, reg); oldErr == nil {
				p, err, e.schema = old, nil, schema
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	if results != nil {
		contract, err := lmcode.ResultContractFromSchema(e.schema)
		if err != nil {
			return nil, err
		}
		if e.result, err = results.Select(0, reg, contract); err != nil {
			return nil, err
		}
	}
	entries := map[string]int{}
	for i, v := range p.Entries() {
		if v.Annotation == "" || v.Iface != "interface Handler(Exchange) -> void" || len(v.Params) != 1 || v.Params[0] != "Exchange" || v.Returns != "void" {
			return nil, fmt.Errorf("entry %s must have an annotation and implement Handler(Exchange) -> void", v.Name)
		}
		if _, ok := entries[v.Annotation]; ok {
			return nil, fmt.Errorf("duplicate annotation @%s", v.Annotation)
		}
		entries[v.Annotation] = i
	}
	// Dry mode validates handlers but never runs initialization closures.
	if results != nil && results.Dry() {
		return e, nil
	}
	if err = p.Run(lmcode.NewContext(), reg); err != nil {
		return nil, err
	}
	if len(e.routes) == 0 {
		return nil, errors.New("script must register at least one route")
	}
	for i := range e.routes {
		r := &e.routes[i]
		idx, ok := entries[r.annotation]
		if !ok {
			return nil, fmt.Errorf("route references missing annotation @%s", r.annotation)
		}
		r.entry = idx
		if err = e.register(*r); err != nil {
			return nil, err
		}
	}
	return e, nil
}
func (e *Engine) register(r route) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("invalid or conflicting route %s %s: %v", r.method, r.path, v)
		}
	}()
	e.mux.HandleFunc(r.method+" "+r.path, func(w http.ResponseWriter, q *http.Request) { e.handle(r, w, q) })
	return nil
}
func (e *Engine) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w, record := e.beginRequestLog(w, r)
	defer record()
	start := time.Now()
	_, pattern := e.mux.Handler(r)
	kind, upstream, failure := "mocked", "", ""
	if e.options.AuditWriter != nil {
		response := &auditResponse{ResponseWriter: w}
		w = response
		defer func() { e.audit(r, response, kind, upstream, failure, start) }()
	}
	if pattern == "" && e.forwarder != nil {
		// An empty pattern can mean a method mismatch: retain the mux's 405/Allow.
		probe := r.Clone(r.Context())
		for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			probe.Method = method
			if _, matched := e.mux.Handler(probe); matched != "" {
				pattern = matched
				break
			}
		}
	}
	if pattern == "" && e.forwarder != nil {
		kind = "forwarded"
		var abort bool
		upstream, failure, abort = e.forwarder.serve(w, r)
		if abort {
			panic(http.ErrAbortHandler)
		}
		return
	}
	if pattern == "" {
		kind = "unmatched"
	}
	e.mux.ServeHTTP(w, r)
}
func (e *Engine) handle(route route, w http.ResponseWriter, r *http.Request) {
	limitWriter := w
	for {
		wrapper, ok := limitWriter.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			break
		}
		limitWriter = wrapper.Unwrap()
	}
	body, err := io.ReadAll(http.MaxBytesReader(limitWriter, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "request body exceeds 1 MiB or cannot be read", http.StatusRequestEntityTooLarge)
		return
	}
	x := &exchange{request: r, body: string(body), status: 200, headers: make(http.Header), state: e.state, stream: &responseStream{writer: w}}
	// A fresh registry isolates VM arenas and execution locks between concurrent requests.
	reg := registry(e.options, e.state, func(string, string, string) error { return errors.New("route() is only available during startup") })
	p, err := lmcode.Load(e.code, e.schema, reg)
	if err == nil {
		_, err = p.Call(route.entry, lmcode.NewContext(), reg, []lmcode.Value{lmcode.Object("Exchange", x)})
	}
	if err != nil {
		if x.stream.committed {
			markStreamFailure(w, "lmcode handler failed after stream commit")
			if !x.stream.closed {
				panic(http.ErrAbortHandler)
			}
			return
		}
		http.Error(w, "lmcode handler error: "+err.Error(), 500)
		return
	}
	if x.stream.closed {
		return
	}
	if x.stream.committed && !x.drop {
		return
	}
	if x.delay > 0 {
		timer := time.NewTimer(x.delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-r.Context().Done():
			return
		}
	}
	if x.drop {
		markStreamFailure(w, "disconnect")
		c, _, err := http.NewResponseController(w).Hijack()
		if err == nil {
			_ = c.Close()
		} else {
			http.Error(w, "connection cannot be hijacked", 500)
		}
		return
	}
	for k, vs := range x.headers {
		w.Header()[k] = vs
	}
	w.WriteHeader(x.status)
	_, _ = io.WriteString(w, x.output)
}
func registry(options Options, _ *store, add func(string, string, string) error) *lmcode.Registry {
	reg := lmcode.NewRegistry()
	helpers(reg, options)
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(reg.Function("route", []string{"string", "string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) { return lmcode.None(), add(a[0].Str, a[1].Str, a[2].Str) }))
	method := func(name string, params []string, ret string, f func(*exchange, []lmcode.Value) (lmcode.Value, error)) {
		must(reg.Method("Exchange", name, params, ret, func(self any, a []lmcode.Value) (lmcode.Value, error) {
			x, ok := self.(*exchange)
			if !ok || x == nil {
				return lmcode.None(), errors.New("invalid exchange")
			}
			return f(x, a)
		}))
	}
	text := func(name string, params []string, f func(*exchange, []lmcode.Value) string) {
		method(name, params, "string", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) { return lmcode.String(f(x, a)), nil })
	}
	text("request_json", nil, func(x *exchange, _ []lmcode.Value) string {
		h := map[string]string{}
		for k, v := range x.request.Header {
			if len(v) > 0 {
				h[strings.ToLower(k)] = v[0]
			}
		}
		b, _ := json.Marshal(map[string]any{"method": x.request.Method, "url": x.request.URL.RequestURI(), "headers": h, "body": x.body})
		return string(b)
	})
	text("method", nil, func(x *exchange, _ []lmcode.Value) string { return x.request.Method })
	text("path", nil, func(x *exchange, _ []lmcode.Value) string { return x.request.URL.Path })
	text("query", []string{"string"}, func(x *exchange, a []lmcode.Value) string { return x.request.URL.Query().Get(a[0].Str) })
	text("param", []string{"string"}, func(x *exchange, a []lmcode.Value) string { return x.request.PathValue(a[0].Str) })
	text("header", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		return x.lookupHeader(a[0].Str)
	})
	text("body", nil, func(x *exchange, _ []lmcode.Value) string { return x.body })
	method("status", []string{"int"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if x.stream != nil && x.stream.committed {
			return lmcode.None(), errors.New("response already streaming")
		}
		if a[0].Int < 200 || a[0].Int > 599 {
			return lmcode.None(), errors.New("status must be 200..599")
		}
		x.status = int(a[0].Int)
		return lmcode.None(), nil
	})
	method("set_header", []string{"string", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if a[0].Str == "" || strings.ContainsAny(a[0].Str, "\r\n :\t") || strings.ContainsAny(a[1].Str, "\r\n") {
			return lmcode.None(), errors.New("invalid response header")
		}
		if x.stream != nil && x.stream.committed {
			if x.headers.Get(a[0].Str) == a[1].Str {
				return lmcode.None(), nil
			}
			return lmcode.None(), errors.New("response already streaming")
		}
		x.headers.Set(a[0].Str, a[1].Str)
		return lmcode.None(), nil
	})
	streamMethods(method)
	observabilityMethods(method)
	for _, name := range []string{"send", "json"} {
		method(name, []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
			if x.stream != nil && x.stream.committed {
				return lmcode.None(), errors.New("response already streaming")
			}
			if x.responseQueue != "" {
				enqueueResponse(x, "J"+a[0].Str)
				x.status = http.StatusAccepted
				return lmcode.None(), nil
			}
			x.output = a[0].Str
			if name == "json" {
				x.headers.Set("Content-Type", "application/json")
			}
			return lmcode.None(), nil
		})
	}
	method("delay", []string{"int"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if x.stream != nil && x.stream.committed {
			return lmcode.None(), errors.New("response already streaming")
		}
		if a[0].Int < 0 || a[0].Int > 60000 {
			return lmcode.None(), errors.New("delay must be 0..60000 milliseconds")
		}
		x.delay = time.Duration(a[0].Int) * time.Millisecond
		return lmcode.None(), nil
	})
	method("pause", []string{"int"}, "bool", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		result, err := pauseContext(x.request.Context(), a[0].Int)
		if err != nil {
			return lmcode.None(), err
		}
		return lmcode.Bool(result), nil
	})
	method("disconnect", nil, "void", func(x *exchange, _ []lmcode.Value) (lmcode.Value, error) { x.drop = true; return lmcode.None(), nil })
	method("queue_response", []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if a[0].Str == "" {
			return lmcode.None(), errors.New("response queue session is empty")
		}
		x.responseQueue = a[0].Str
		return lmcode.None(), nil
	})
	text("next_queued_response", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		if a[0].Str == "" {
			return ""
		}
		return dequeueResponse(x.state, a[0].Str)
	})
	text("take", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		x.state.Lock()
		defer x.state.Unlock()
		v := x.state.values[a[0].Str]
		delete(x.state.values, a[0].Str)
		return v
	})
	text("get", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		return x.state.get(a[0].Str)
	})
	method("set", []string{"string", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		x.state.Lock()
		defer x.state.Unlock()
		x.state.values[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	})
	method("increment", []string{"string"}, "int", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.Int(x.state.increment(a[0].Str)), nil
	})
	return reg
}

// Server owns a real loopback listener, including port 0 allocation.
type Server struct {
	URL         string
	CACert      string
	caDirectory string
	closeOnce   sync.Once
	closeError  error
	http        *http.Server
	done        chan error
}

func Start(e *Engine, address string) (*Server, error) {
	listener, err := listenLoopback(address)
	if err != nil {
		return nil, err
	}
	return serveListener(e, listener, "http"), nil
}
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		defer func() {
			if s.caDirectory != "" {
				_ = os.RemoveAll(s.caDirectory)
			}
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.closeError = s.http.Shutdown(ctx)
		if s.closeError != nil {
			_ = s.http.Close()
		}
		<-s.done
	})
	return s.closeError
}

// SetClientOutput records captured stdout after the owned client exits successfully.
func (e *Engine) SetClientOutput(stdout string) error {
	if e.result == nil {
		return errors.New("missing invocation result sink")
	}
	if err := e.result.Put("stdout", lmcode.String(stdout)); err != nil {
		return err
	}
	return e.result.Put("code", lmcode.Int(0))
}
