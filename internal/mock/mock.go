// Package mock binds verified lmcode entries to HTTP routes.
package mock

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	lmcode "github.com/lmctl-ai/lmcode-go"
)

//go:embed schema.json
var Schema []byte

type route struct {
	method, path, annotation string
	entry                    int
}
type store struct {
	sync.Mutex
	values map[string]string
	counts map[string]int64
}
type Engine struct {
	options   Options
	code      []byte
	routes    []route
	state     *store
	mux       *http.ServeMux
	forwarder *forwarder
	auditMu   sync.Mutex
}
type exchange struct {
	request *http.Request
	body    string
	status  int
	headers http.Header
	output  string
	delay   time.Duration
	drop    bool
	state   *store
}

// New verifies all entries and route bindings before opening any network listener.
func New(code []byte) (*Engine, error) { return NewWithOptions(code, Options{}) }

func NewWithOptions(code []byte, options Options) (*Engine, error) {
	e := &Engine{options: options, code: append([]byte(nil), code...), state: &store{values: map[string]string{}, counts: map[string]int64{}}, mux: http.NewServeMux()}
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
	p, err := lmcode.Load(e.code, Schema, reg)
	if err != nil {
		return nil, err
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
	if audited, ok := w.(*auditResponse); ok {
		// MaxBytesReader needs the native writer's private requestTooLarge hook.
		limitWriter = audited.ResponseWriter
	}
	body, err := io.ReadAll(http.MaxBytesReader(limitWriter, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "request body exceeds 1 MiB or cannot be read", http.StatusRequestEntityTooLarge)
		return
	}
	x := &exchange{request: r, body: string(body), status: 200, headers: make(http.Header), state: e.state}
	// A fresh registry isolates VM arenas and execution locks between concurrent requests.
	reg := registry(e.options, e.state, func(string, string, string) error { return errors.New("route() is only available during startup") })
	p, err := lmcode.Load(e.code, Schema, reg)
	if err == nil {
		_, err = p.Call(route.entry, lmcode.NewContext(), reg, []lmcode.Value{lmcode.Object("Exchange", x)})
	}
	if err != nil {
		http.Error(w, "lmcode handler error: "+err.Error(), 500)
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
		// net/http promotes Host out of Header when parsing an incoming request.
		if strings.EqualFold(a[0].Str, "Host") {
			return x.request.Host
		}
		return x.request.Header.Get(a[0].Str)
	})
	text("body", nil, func(x *exchange, _ []lmcode.Value) string { return x.body })
	method("status", []string{"int"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
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
		x.headers.Set(a[0].Str, a[1].Str)
		return lmcode.None(), nil
	})
	for _, name := range []string{"send", "json"} {
		method(name, []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
			x.output = a[0].Str
			if name == "json" {
				x.headers.Set("Content-Type", "application/json")
			}
			return lmcode.None(), nil
		})
	}
	method("delay", []string{"int"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if a[0].Int < 0 || a[0].Int > 60000 {
			return lmcode.None(), errors.New("delay must be 0..60000 milliseconds")
		}
		x.delay = time.Duration(a[0].Int) * time.Millisecond
		return lmcode.None(), nil
	})
	method("disconnect", nil, "void", func(x *exchange, _ []lmcode.Value) (lmcode.Value, error) { x.drop = true; return lmcode.None(), nil })
	text("take", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		x.state.Lock()
		defer x.state.Unlock()
		v := x.state.values[a[0].Str]
		delete(x.state.values, a[0].Str)
		return v
	})
	text("get", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		x.state.Lock()
		defer x.state.Unlock()
		return x.state.values[a[0].Str]
	})
	method("set", []string{"string", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		x.state.Lock()
		defer x.state.Unlock()
		x.state.values[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	})
	method("increment", []string{"string"}, "int", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		x.state.Lock()
		defer x.state.Unlock()
		x.state.counts[a[0].Str]++
		return lmcode.Int(x.state.counts[a[0].Str]), nil
	})
	return reg
}

// Server owns a real loopback listener, including port 0 allocation.
type Server struct {
	URL  string
	http *http.Server
	done chan error
}

func Start(e *Engine, address string) (*Server, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("listen address must be a loopback IP (127.0.0.1 or ::1)")
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}
	s := &Server{URL: "http://" + listener.Addr().String(), http: &http.Server{Handler: e, DisableGeneralOptionsHandler: e.forwarder != nil, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}, done: make(chan error, 1)}
	go func() {
		err := s.http.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.done <- err
	}()
	return s, nil
}
func (s *Server) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.http.Shutdown(ctx)
	if err != nil {
		_ = s.http.Close()
	}
	<-s.done
	return err
}
