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
	"net/http/httptest"
	"net/url"
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
	method, path, annotation, objType string
	entry                             int
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
	result         *lmcode.ResultSink
	options        Options
	code           []byte
	schema         []byte
	routes         []route
	registered     map[string]bool
	cliOptions     map[string]int
	prefix         string
	stopped        bool
	state          *store
	mux            *http.ServeMux
	forwarder      *forwarder
	auditMu        sync.Mutex
}

func stdRestMethod(iface string) string {
	switch iface {
	case "interface RestGet(RestExchange) -> void":
		return "GET"
	case "interface RestPost(RestExchange) -> void":
		return "POST"
	case "interface RestPut(RestExchange) -> void":
		return "PUT"
	case "interface RestDelete(RestExchange) -> void":
		return "DELETE"
	case "interface RestPatch(RestExchange) -> void":
		return "PATCH"
	case "interface RestHead(RestExchange) -> void":
		return "HEAD"
	case "interface RestOptions(RestExchange) -> void":
		return "OPTIONS"
	default:
		return ""
	}
}

func isStdRestOrCliIface(iface string) bool {
	return iface == "interface RestHandler(RestExchange) -> void" ||
		iface == "interface CliCommand(RestExchange) -> void" ||
		iface == "interface CliOption(RestExchange) -> void" ||
		stdRestMethod(iface) != ""
}

func normalizeMuxPath(prefix, path string) string {
	if prefix != "" {
		p := prefix
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		for len(p) > 1 && strings.HasSuffix(p, "/") {
			p = p[:len(p)-1]
		}
		if p != "/" && !strings.HasPrefix(path, p) {
			if path == "/" {
				path = p
			} else if strings.HasPrefix(path, "/") {
				path = p + path
			} else {
				path = p + "/" + path
			}
		}
	}
	if !strings.HasPrefix(path, "/") {
		return path
	}
	segs := strings.Split(path, "/")
	for i := 1; i < len(segs); i++ {
		s := segs[i]
		if len(s) > 1 && s[0] == ':' {
			segs[i] = "{" + s[1:] + "}"
		} else if len(s) > 1 && s[0] == '*' {
			segs[i] = "{" + s[1:] + "...}"
		}
	}
	return strings.Join(segs, "/")
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
	e := &Engine{
		options:    options,
		code:       append([]byte(nil), code...),
		registered: map[string]bool{},
		cliOptions: map[string]int{},
		state:      &store{values: map[string]string{}, counts: map[string]int64{}},
		mux:        http.NewServeMux(),
	}
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
	addRoute := func(method, path, annotation string) error {
		path = normalizeMuxPath(e.prefix, path)
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
	}
	reg := e.newRegistry(addRoute)
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
	seenKey := map[string]bool{}
	for i, v := range p.Entries() {
		isHandler := v.Iface == "interface Handler(Exchange) -> void" && len(v.Params) == 1 && v.Params[0] == "Exchange" && v.Returns == "void"
		isStd := isStdRestOrCliIface(v.Iface) && len(v.Params) == 1 && v.Params[0] == "RestExchange" && v.Returns == "void"
		if v.Annotation == "" || (!isHandler && !isStd) {
			return nil, fmt.Errorf("entry %s must have an annotation and implement Handler(Exchange) -> void", v.Name)
		}
		dedupKey := v.Annotation
		if m := stdRestMethod(v.Iface); m != "" && strings.HasPrefix(v.Annotation, "/") {
			dedupKey = m + " " + v.Annotation
		}
		if seenKey[dedupKey] {
			return nil, fmt.Errorf("duplicate annotation @%s", v.Annotation)
		}
		seenKey[dedupKey] = true
		if _, ok := entries[v.Annotation]; !ok {
			entries[v.Annotation] = i
		}
	}
	// Dry mode validates handlers but never runs initialization closures.
	if results != nil && results.Dry() {
		return e, nil
	}
	if err = p.Run(lmcode.NewContext(), reg); err != nil {
		return nil, err
	}
	if err = e.finalizeRoutes(p, entries); err != nil {
		return nil, err
	}
	if len(e.routes) == 0 && len(e.cliOptions) == 0 {
		return nil, errors.New("script must register at least one route")
	}
	return e, nil
}

func (e *Engine) finalizeRoutes(p *lmcode.Program, entries map[string]int) error {
	for i, v := range p.Entries() {
		if !isStdRestOrCliIface(v.Iface) {
			continue
		}
		ann := v.Annotation
		if v.Iface == "interface CliOption(RestExchange) -> void" {
			e.cliOptions[ann] = i
			if stripped := strings.TrimLeft(ann, "-"); stripped != "" {
				e.cliOptions[stripped] = i
			}
			continue
		}
		var method, path string
		if m := stdRestMethod(v.Iface); m != "" {
			if sp := strings.IndexByte(ann, ' '); sp >= 0 {
				method, path = ann[:sp], ann[sp+1:]
			} else if strings.HasPrefix(ann, "/") {
				method, path = m, ann
			} else {
				continue
			}
		} else if v.Iface == "interface RestHandler(RestExchange) -> void" {
			if sp := strings.IndexByte(ann, ' '); sp >= 0 {
				method, path = ann[:sp], ann[sp+1:]
			} else if strings.HasPrefix(ann, "/") {
				method, path = "GET", ann
			} else {
				continue
			}
		} else if v.Iface == "interface CliCommand(RestExchange) -> void" {
			if sp := strings.IndexByte(ann, ' '); sp >= 0 {
				method, path = ann[:sp], ann[sp+1:]
			} else {
				method, path = "GET", ann
			}
			if !strings.HasPrefix(path, "/") {
				path = "/" + path
			}
		}
		path = normalizeMuxPath(e.prefix, path)
		already := false
		for _, r := range e.routes {
			if r.method == method && r.path == path && r.entry == i {
				already = true
				break
			}
		}
		if already {
			continue
		}
		for _, r := range e.routes {
			if r.method == method && r.path == path {
				return fmt.Errorf("duplicate route %s %s", method, path)
			}
		}
		e.routes = append(e.routes, route{
			method:     method,
			path:       path,
			annotation: fmt.Sprintf("__auto_%d", i),
			objType:    "RestExchange",
			entry:      i,
		})
	}
	allEntries := p.Entries()
	for i := range e.routes {
		r := &e.routes[i]
		if r.objType == "" {
			idx, ok := entries[r.annotation]
			if !ok {
				return fmt.Errorf("route references missing annotation @%s", r.annotation)
			}
			r.entry = idx
			if isStdRestOrCliIface(allEntries[idx].Iface) {
				r.objType = "RestExchange"
			} else {
				r.objType = "Exchange"
			}
		}
		r.path = normalizeMuxPath(e.prefix, r.path)
		key := r.method + " " + r.path
		if !e.registered[key] {
			if err := e.register(*r); err != nil {
				return err
			}
			e.registered[key] = true
		}
	}
	return nil
}

// CLI dispatches stateless CLI arguments against the engine's routes and CliOption/CliCommand entries.
func (e *Engine) CLI(argv []string) (string, error) {
	if e.schema == nil {
		e.schema = Schema
	}
	if len(e.registered) == 0 {
		reg := e.newRegistry(func(string, string, string) error { return nil })
		if p, err := lmcode.Load(e.code, e.schema, reg); err == nil {
			entries := map[string]int{}
			for i, v := range p.Entries() {
				if _, ok := entries[v.Annotation]; !ok {
					entries[v.Annotation] = i
				}
			}
			if err := e.finalizeRoutes(p, entries); err != nil {
				return "", err
			}
		}
	}
	method, body, methodExplicit := "GET", "", false
	var positionals []string
	type optVal struct{ flag, key, val string }
	var parsedOpts []optVal
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if arg == "--" {
			if body == "" && i+1 < len(argv) {
				body = strings.Join(argv[i+1:], " ")
			}
			break
		}
		if len(arg) > 1 && arg[0] == '-' {
			flagName, key, val := arg, "", "true"
			if eq := strings.IndexByte(arg, '='); eq >= 0 {
				flagName, val = arg[:eq], arg[eq+1:]
				key = strings.TrimLeft(flagName, "-")
			} else {
				key = strings.TrimLeft(flagName, "-")
				if i+1 < len(argv) && (argv[i+1] == "" || argv[i+1][0] != '-') {
					next := argv[i+1]
					isSpecial := key == "method" || key == "X" || key == "body" || key == "d"
					probePath := next
					if !strings.HasPrefix(probePath, "/") {
						probePath = "/" + probePath
					}
					probePath = normalizeMuxPath(e.prefix, probePath)
					_, pat := e.mux.Handler(httptest.NewRequest("GET", probePath, nil))
					if !(len(positionals) == 0 && !isSpecial && pat != "") {
						i++
						val = argv[i]
					}
				}
			}
			if key == "method" || key == "X" {
				method, methodExplicit = strings.ToUpper(val), true
			} else if key == "body" || key == "d" {
				body = val
			}
			parsedOpts = append(parsedOpts, optVal{flagName, key, val})
		} else {
			positionals = append(positionals, arg)
		}
	}
	path := "/"
	if len(positionals) > 0 {
		var joined string
		for i, seg := range positionals {
			if i == 0 && strings.HasPrefix(seg, "/") {
				joined = seg
			} else {
				joined += "/" + seg
			}
		}
		if joined != "" {
			path = joined
		}
	}
	path = normalizeMuxPath(e.prefix, path)
	q := url.Values{}
	e.state.Lock()
	for _, opt := range parsedOpts {
		q.Set(opt.key, opt.val)
		q.Set(opt.flag, opt.val)
		e.state.values[opt.key] = opt.val
	}
	e.state.Unlock()
	target := path
	if enc := q.Encode(); enc != "" {
		target += "?" + enc
	}
	var optOutput string
	for _, opt := range parsedOpts {
		idx, ok := e.cliOptions[opt.flag]
		if !ok {
			idx, ok = e.cliOptions[opt.key]
		}
		if ok {
			req := httptest.NewRequest(method, target, strings.NewReader(body))
			rec := httptest.NewRecorder()
			x := &exchange{request: req, body: body, status: 200, headers: make(http.Header), state: e.state, stream: &responseStream{writer: rec}}
			reg := e.newRegistry(func(string, string, string) error { return errors.New("route() is only available during startup") })
			p, err := lmcode.Load(e.code, e.schema, reg)
			if err != nil {
				return "", err
			}
			if _, err := p.Call(idx, lmcode.NewContext(), reg, []lmcode.Value{lmcode.Object("RestExchange", x)}); err != nil {
				return "", err
			}
			if x.output != "" {
				optOutput = x.output
			}
		}
	}
	if e.stopped {
		return optOutput, nil
	}
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if _, pat := e.mux.Handler(req); pat == "" && !methodExplicit {
		candidates := []string{"POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
		for _, cand := range candidates {
			probe := httptest.NewRequest(cand, target, strings.NewReader(body))
			if _, p := e.mux.Handler(probe); p != "" {
				req = probe
				break
			}
		}
	}
	if _, pat := e.mux.Handler(req); pat == "" {
		if len(positionals) == 0 && optOutput != "" {
			return optOutput, nil
		}
		return "Not Found", nil
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	if rec.Code == 204 || rec.Code == 304 {
		return "", nil
	}
	return rec.Body.String(), nil
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
	reg := e.newRegistry(func(string, string, string) error { return errors.New("route() is only available during startup") })
	p, err := lmcode.Load(e.code, e.schema, reg)
	if err == nil {
		objType := route.objType
		if objType == "" {
			objType = "Exchange"
		}
		_, err = p.Call(route.entry, lmcode.NewContext(), reg, []lmcode.Value{lmcode.Object(objType, x)})
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

// Signatures returns the canonical list of standard and host signatures.
func Signatures() []string {
	e := &Engine{state: &store{values: map[string]string{}, counts: map[string]int64{}}}
	return e.newRegistry(func(string, string, string) error { return errors.New("introspection") }).Signatures()
}

func (e *Engine) newRegistry(add func(string, string, string) error) *lmcode.Registry {
	reg := registry(e.options, e.state, add)
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	none := lmcode.None()
	dryNone := lmcode.Function(func([]lmcode.Value) (lmcode.Value, error) { return none, nil })
	must(reg.OverrideStdFunction("rest_config", []string{"string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) {
		e.state.Lock()
		e.state.values[a[0].Str] = a[1].Str
		e.state.Unlock()
		if a[0].Str == "prefix" || a[0].Str == "base_path" {
			e.prefix = a[1].Str
		}
		return none, nil
	}, lmcode.Dry(dryNone)))
	must(reg.OverrideStdFunction("rest_route", []string{"string", "string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) {
		return none, add(a[0].Str, a[1].Str, a[2].Str)
	}, lmcode.Dry(dryNone)))
	must(reg.OverrideStdFunction("rest_serve", nil, "void", func([]lmcode.Value) (lmcode.Value, error) {
		return none, nil
	}, lmcode.Dry(dryNone)))
	must(reg.OverrideStdFunction("rest_stop", nil, "void", func([]lmcode.Value) (lmcode.Value, error) {
		e.stopped = true
		return none, nil
	}, lmcode.Dry(dryNone)))
	must(reg.OverrideStdFunction("rest_cli", []string{"StrList"}, "string", func(a []lmcode.Value) (lmcode.Value, error) {
		list, ok := a[0].Obj.(*lmcode.StrList)
		if !ok || list == nil {
			return lmcode.String(""), lmcode.NewHostError("rest_cli: invalid StrList")
		}
		out, err := e.CLI(list.Values())
		return lmcode.String(out), err
	}, lmcode.Dry(lmcode.Function(func([]lmcode.Value) (lmcode.Value, error) { return lmcode.String(""), nil }))))
	return reg
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
	stdMethod := func(name string, params []string, ret string, f func(*exchange, []lmcode.Value) (lmcode.Value, error)) {
		cb := lmcode.Method(func(self any, a []lmcode.Value) (lmcode.Value, error) {
			x, ok := self.(*exchange)
			if !ok || x == nil {
				return lmcode.None(), errors.New("invalid exchange")
			}
			return f(x, a)
		})
		must(reg.OverrideStdMethod("RestExchange", name, params, ret, cb, lmcode.Dry(cb)))
	}
	bothMethod := func(name string, params []string, ret string, f func(*exchange, []lmcode.Value) (lmcode.Value, error)) {
		method(name, params, ret, f)
		stdMethod(name, params, ret, f)
	}
	text := func(name string, params []string, f func(*exchange, []lmcode.Value) string) {
		method(name, params, "string", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) { return lmcode.String(f(x, a)), nil })
	}
	bothText := func(name string, params []string, f func(*exchange, []lmcode.Value) string) {
		bothMethod(name, params, "string", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) { return lmcode.String(f(x, a)), nil })
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
	bothText("method", nil, func(x *exchange, _ []lmcode.Value) string { return x.request.Method })
	bothText("path", nil, func(x *exchange, _ []lmcode.Value) string { return x.request.URL.Path })
	bothText("query", []string{"string"}, func(x *exchange, a []lmcode.Value) string { return x.request.URL.Query().Get(a[0].Str) })
	stdMethod("has_query", []string{"string"}, "bool", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		_, ok := x.request.URL.Query()[a[0].Str]
		return lmcode.Bool(ok), nil
	})
	bothText("param", []string{"string"}, func(x *exchange, a []lmcode.Value) string { return x.request.PathValue(a[0].Str) })
	bothText("header", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		return x.lookupHeader(a[0].Str)
	})
	stdMethod("has_header", []string{"string"}, "bool", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if strings.EqualFold(a[0].Str, "Host") {
			return lmcode.Bool(x.request.Host != ""), nil
		}
		return lmcode.Bool(len(x.request.Header.Values(a[0].Str)) > 0), nil
	})
	bothText("body", nil, func(x *exchange, _ []lmcode.Value) string { return x.body })
	bothMethod("status", []string{"int"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if x.stream != nil && x.stream.committed {
			return lmcode.None(), errors.New("response already streaming")
		}
		if a[0].Int < 200 || a[0].Int > 599 {
			return lmcode.None(), errors.New("status must be 200..599")
		}
		x.status = int(a[0].Int)
		return lmcode.None(), nil
	})
	bothMethod("set_header", []string{"string", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
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
		name := name
		bothMethod(name, []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
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
	bothText("get", []string{"string"}, func(x *exchange, a []lmcode.Value) string {
		return x.state.get(a[0].Str)
	})
	bothMethod("set", []string{"string", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		x.state.Lock()
		defer x.state.Unlock()
		x.state.values[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	})
	bothMethod("increment", []string{"string"}, "int", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
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
