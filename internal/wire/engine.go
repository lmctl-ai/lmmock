// Package wire dispatches protocol conversations to lmcode SAM entries.
// Applications are bindings and handlers, not switches in a Go server.
package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"unicode/utf8"

	lmcode "github.com/lmctl-ai/lmcode-go"
	"github.com/lmctl-ai/lmmock/internal/proto"
)

// Session owns framing for one conversation. EOF is peer half-close, not a timer.
// Send must flush before returning. Callbacks must unblock when their layer stops.
type Session interface {
	Receive() (kind, body string, err error)
	Send(kind, body string) error
	Header(name string) string
	SetHeader(name, value string) error
	Fail(code int, message string) error
}

// Layer validates bindings before listening. A new protocol supplies this small
// adapter and a SAM name; the engine and CLI need no application-specific code.
type Layer interface {
	Scheme() string
	ValidateRoute(string) error
	Serve(context.Context, net.Listener, *Engine) error
}
type Definition struct {
	SAM string
	New func(base string, options json.RawMessage) (Layer, error)
}

var definitions = map[string]Definition{}
var definitionsMu sync.RWMutex

func Register(name string, d Definition) {
	definitionsMu.Lock()
	defer definitionsMu.Unlock()
	if name == "" || d.SAM == "" || d.New == nil {
		panic("invalid wire definition")
	}
	if _, ok := definitions[name]; ok {
		panic("duplicate wire definition: " + name)
	}
	definitions[name] = d
	// Existing proto commands can serve the same configured adapter. Their paths
	// are cwd-relative; Open uses the scenario file's directory instead.
	proto.Register(name, func(raw json.RawMessage) (proto.Server, error) {
		base, e := os.Getwd()
		if e != nil {
			return nil, e
		}
		return configured(name, base, raw)
	})
}
func definition(name string) (Definition, error) {
	definitionsMu.RLock()
	defer definitionsMu.RUnlock()
	d, ok := definitions[name]
	if !ok {
		return d, fmt.Errorf("unknown lmcode protocol %q", name)
	}
	return d, nil
}
func Names() []string {
	definitionsMu.RLock()
	defer definitionsMu.RUnlock()
	out := []string{}
	for n := range definitions {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Decode rejects misspelled protocol/configuration fields rather than ignoring them.
func Decode(raw []byte, value any) error {
	if len(raw) == 0 {
		raw = []byte(`{}`)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("configuration must contain one JSON value")
	}
	return nil
}

type Config struct {
	Program  string          `json:"program"`
	Protocol json.RawMessage `json:"protocol"`
	Data     json.RawMessage `json:"data"`
}

func Open(kind, file string) (*Engine, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return configured(kind, filepath.Dir(file), raw)
}
func configured(kind, base string, raw []byte) (*Engine, error) {
	var cfg Config
	if err := Decode(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Program == "" {
		return nil, errors.New("scenario requires program")
	}
	d, err := definition(kind)
	if err != nil {
		return nil, err
	}
	layer, err := d.New(base, cfg.Protocol)
	if err != nil {
		return nil, err
	}
	file := Resolve(base, cfg.Program)
	code, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return New(kind, layer, code, cfg.Data)
}
func Resolve(base, file string) string {
	if filepath.IsAbs(file) {
		return file
	}
	return filepath.Join(base, file)
}

type signature struct {
	Params  []string `json:"params"`
	Returns string   `json:"returns"`
}

func sig(ret string, params ...string) []signature {
	if params == nil {
		params = []string{}
	}
	return []signature{{params, ret}}
}
func Schema(kind string) ([]byte, error) {
	d, err := definition(kind)
	if err != nil {
		return nil, err
	}
	methods := map[string]any{"status": sig("void", "int"), "recv": sig("bool"), "body": sig("string"), "frame": sig("string"), "method": sig("string"), "send": sig("void", "string"), "send_binary": sig("void", "string"), "header": sig("string", "string"), "set_header": sig("void", "string", "string"), "fail": sig("void", "int", "string")}
	schema := map[string]any{"dsl.schema": 2, "name": "lmmock_" + kind, "types": map[string]any{"Session": map[string]any{"methods": methods}}, "context": map[string]any{}, "functions": map[string]any{"bind": sig("void", "string", "string"), "config": sig("string"), "json_quote": sig("string", "string"), "state_get": sig("string", "string"), "state_put": sig("void", "string", "string"), "count": sig("int", "string")}, "interfaces": map[string]any{d.SAM: map[string]any{"params": []map[string]string{{"name": "session", "type": "Session"}}, "returns": "void"}}}
	b, err := json.Marshal(schema)
	return append(b, '\n'), err
}

type Engine struct {
	layer              Layer
	code, schema, data []byte
	bindings           map[string]int
	mu                 sync.Mutex
	values             map[string]string
	counts             map[string]int64
}
type binding struct{ key, annotation string }

func New(kind string, layer Layer, code, data []byte) (*Engine, error) {
	d, err := definition(kind)
	if err != nil {
		return nil, err
	}
	schema, err := Schema(kind)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		data = []byte(`{}`)
	}
	if !json.Valid(data) {
		return nil, errors.New("invalid scenario data")
	}
	e := &Engine{layer: layer, code: append([]byte(nil), code...), schema: schema, data: append([]byte(nil), data...), bindings: map[string]int{}, values: map[string]string{}, counts: map[string]int64{}}
	var bindings []binding
	reg := e.registry(func(key, annotation string) error {
		if err := layer.ValidateRoute(key); err != nil {
			return err
		}
		for _, b := range bindings {
			if key == b.key {
				return fmt.Errorf("duplicate binding %q", key)
			}
		}
		bindings = append(bindings, binding{key, annotation})
		return nil
	})
	program, err := lmcode.Load(e.code, schema, reg)
	if err != nil {
		return nil, err
	}
	entries := map[string]int{}
	for index, entry := range program.Entries() {
		if entry.Annotation == "" || entry.Iface != "interface "+d.SAM+"(Session) -> void" {
			return nil, fmt.Errorf("entry %s must implement %s(Session) -> void", entry.Name, d.SAM)
		}
		if _, ok := entries[entry.Annotation]; ok {
			return nil, fmt.Errorf("duplicate annotation @%s", entry.Annotation)
		}
		entries[entry.Annotation] = index
	}
	if err = program.Run(lmcode.NewContext(), reg); err != nil {
		return nil, err
	}
	if len(bindings) == 0 {
		return nil, errors.New("program must bind at least one handler")
	}
	for _, b := range bindings {
		index, ok := entries[b.annotation]
		if !ok {
			return nil, fmt.Errorf("binding %q names missing annotation @%s", b.key, b.annotation)
		}
		e.bindings[b.key] = index
	}
	return e, nil
}
func (e *Engine) Scheme() string                                   { return e.layer.Scheme() }
func (e *Engine) Serve(ctx context.Context, ln net.Listener) error { return e.layer.Serve(ctx, ln, e) }
func (e *Engine) Has(key string) bool {
	_, ok := e.bindings[key]
	if !ok {
		_, ok = e.bindings["*"]
	}
	return ok
}

type invocation struct {
	session             Session
	method, body, frame string
	failure             error
	eof                 bool
}

func (e *Engine) Dispatch(key string, s Session) error {
	index, ok := e.bindings[key]
	if !ok {
		index, ok = e.bindings["*"]
	}
	if !ok {
		return fmt.Errorf("unbound method %q", key)
	}
	// A registry/arena belongs to this invocation, not to concurrent connections.
	reg := e.registry(func(string, string) error { return errors.New("bind is startup-only") })
	p, err := lmcode.Load(e.code, e.schema, reg)
	if err != nil {
		return err
	}
	call := &invocation{session: s, method: key}
	_, err = p.Call(index, lmcode.NewContext(), reg, []lmcode.Value{lmcode.Object("Session", call)})
	if call.failure != nil {
		return call.failure
	}
	return err
}
func (e *Engine) registry(bind func(string, string) error) *lmcode.Registry {
	r := lmcode.NewRegistry()
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(r.Function("bind", []string{"string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) { return lmcode.None(), bind(a[0].Str, a[1].Str) }))
	must(r.Function("json_quote", []string{"string"}, "string", func(a []lmcode.Value) (lmcode.Value, error) {
		if !utf8.ValidString(a[0].Str) {
			return lmcode.None(), errors.New("json_quote requires UTF-8")
		}
		b, err := json.Marshal(a[0].Str)
		return lmcode.String(string(b)), err
	}))
	must(r.Function("config", nil, "string", func([]lmcode.Value) (lmcode.Value, error) { return lmcode.String(string(e.data)), nil }))
	must(r.Function("state_get", []string{"string"}, "string", func(a []lmcode.Value) (lmcode.Value, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		return lmcode.String(e.values[a[0].Str]), nil
	}))
	must(r.Function("state_put", []string{"string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.values[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	}))
	must(r.Function("count", []string{"string"}, "int", func(a []lmcode.Value) (lmcode.Value, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.counts[a[0].Str]++
		return lmcode.Int(e.counts[a[0].Str]), nil
	}))
	method := func(name string, params []string, ret string, fn func(*invocation, []lmcode.Value) (lmcode.Value, error)) {
		must(r.Method("Session", name, params, ret, func(self any, a []lmcode.Value) (lmcode.Value, error) {
			x, ok := self.(*invocation)
			if !ok {
				return lmcode.None(), errors.New("invalid session")
			}
			value, err := fn(x, a)
			if err != nil && x.failure == nil {
				x.failure = err
			}
			return value, err
		}))
	}
	method("recv", nil, "bool", func(x *invocation, _ []lmcode.Value) (lmcode.Value, error) {
		if x.eof {
			return lmcode.Bool(false), nil
		}
		kind, body, err := x.session.Receive()
		if errors.Is(err, io.EOF) {
			x.eof = true
			x.body = ""
			x.frame = ""
			return lmcode.Bool(false), nil
		}
		if err != nil {
			return lmcode.None(), err
		}
		x.body = body
		x.frame = kind
		return lmcode.Bool(true), nil
	})
	for _, name := range []string{"body", "frame", "method"} {
		name := name
		method(name, nil, "string", func(x *invocation, _ []lmcode.Value) (lmcode.Value, error) {
			value := x.body
			if name == "frame" {
				value = x.frame
			}
			if name == "method" {
				value = x.method
			}
			return lmcode.String(value), nil
		})
	}
	for _, name := range []string{"send", "send_binary"} {
		name := name
		method(name, []string{"string"}, "void", func(x *invocation, a []lmcode.Value) (lmcode.Value, error) {
			kind := ""
			if name == "send_binary" {
				kind = "binary"
			}
			return lmcode.None(), x.session.Send(kind, a[0].Str)
		})
	}
	method("status", []string{"int"}, "void", func(x *invocation, a []lmcode.Value) (lmcode.Value, error) {
		if writer, ok := x.session.(interface{ SetStatus(int) error }); ok {
			return lmcode.None(), writer.SetStatus(int(a[0].Int))
		}
		return lmcode.None(), errors.New("status is not supported by this protocol")
	})
	method("header", []string{"string"}, "string", func(x *invocation, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.String(x.session.Header(a[0].Str)), nil
	})
	method("set_header", []string{"string", "string"}, "void", func(x *invocation, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), x.session.SetHeader(a[0].Str, a[1].Str)
	})
	method("fail", []string{"int", "string"}, "void", func(x *invocation, a []lmcode.Value) (lmcode.Value, error) {
		x.failure = x.session.Fail(int(a[0].Int), a[1].Str)
		if x.failure == nil {
			x.failure = errors.New("protocol Fail returned no error")
		}
		return lmcode.None(), x.failure
	})
	return r
}

// BaseSession supplies unsupported optional metadata operations for simple wires.
type BaseSession struct{}

func (BaseSession) Header(string) string { return "" }
func (BaseSession) SetHeader(string, string) error {
	return errors.New("headers are not supported by this protocol")
}
func (BaseSession) Fail(code int, message string) error {
	return fmt.Errorf("handler failure %d: %s", code, message)
}

// Signatures is passive introspection: it does not load a program or open a socket.
func Signatures(kind string) ([]string, error) {
	if _, err := definition(kind); err != nil {
		return nil, err
	}
	e := &Engine{}
	return e.registry(func(string, string) error { return errors.New("introspection") }).Signatures(), nil
}
