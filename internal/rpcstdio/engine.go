package rpcstdio

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	lmcode "github.com/lmctl-ai/lmcode-go"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

//go:embed presets/schema*.json presets/*.dslb
var presets embed.FS

func Schema() []byte                     { b, _ := presets.ReadFile("presets/schema.json"); return b }
func Preset(name string) ([]byte, error) { return presets.ReadFile("presets/" + name + ".dslb") }

type Options struct {
	Alias, StateDir string
	Config          []byte
	Stderr          io.Writer
}
type Engine struct {
	deferred map[string]json.RawMessage
	stopped  bool
	exitCode int
	opts     Options
	wire     *Wire
	program  *lmcode.Program
	registry *lmcode.Registry
	routes   map[string]int
	state    map[string]string
	counts   map[string]int64
	pending  map[string]bool
	nextID   int64
	binding  bool
}
type exchange struct {
	deferred  bool
	forward   string
	e         *Engine
	raw       []byte
	method    string
	params    json.RawMessage
	id        json.RawMessage
	responded bool
}

func valid(raw []byte) error { _, e := lmcode.ParseJSON(string(raw)); return e }
func New(code []byte, options Options, wire *Wire) (*Engine, error) {
	if e := valid(options.Config); e != nil {
		return nil, fmt.Errorf("invalid scenario JSON")
	}
	e := &Engine{opts: options, wire: wire, routes: map[string]int{}, state: map[string]string{}, counts: map[string]int64{}, pending: map[string]bool{}, binding: true}
	bindings := map[string]string{}
	r := lmcode.NewRegistry()
	e.registry = r
	e.deferred = map[string]json.RawMessage{}
	if err := e.registerDeferred(); err != nil {
		return nil, err
	}
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(r.Function("json_quote", []string{"string"}, "string", func(a []lmcode.Value) (lmcode.Value, error) {
		b, err := json.Marshal(a[0].Str)
		return lmcode.String(string(b)), err
	}))
	must(r.Function("config", nil, "string", func([]lmcode.Value) (lmcode.Value, error) { return lmcode.String(string(options.Config)), nil }))
	must(r.Function("bind", []string{"string", "string"}, "void", func(a []lmcode.Value) (lmcode.Value, error) {
		if !e.binding {
			return lmcode.None(), errors.New("bind is startup-only")
		}
		if _, ok := bindings[a[0].Str]; ok {
			return lmcode.None(), errors.New("duplicate method binding")
		}
		bindings[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	}))
	method := func(name string, args []string, ret string, fn func(*exchange, []lmcode.Value) (lmcode.Value, error)) {
		must(r.Method("RPC", name, args, ret, func(self any, a []lmcode.Value) (lmcode.Value, error) { return fn(self.(*exchange), a) }))
	}
	text := func(name string, fn func(*exchange) string) {
		method(name, nil, "string", func(x *exchange, _ []lmcode.Value) (lmcode.Value, error) { return lmcode.String(fn(x)), nil })
	}
	text("message", func(x *exchange) string { return string(x.raw) })
	text("method", func(x *exchange) string { return x.method })
	text("params", func(x *exchange) string {
		if len(x.params) == 0 {
			return "{}"
		}
		return string(x.params)
	})
	text("alias", func(*exchange) string { return options.Alias })
	method("get", []string{"string"}, "string", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.String(e.state[a[0].Str]), nil
	})
	method("set", []string{"string", "string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		e.state[a[0].Str] = a[1].Str
		return lmcode.None(), nil
	})
	method("next", []string{"string"}, "int", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		n, err := e.increment(a[0].Str)
		return lmcode.Int(n), err
	})
	method("reply", []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), x.respond("result", json.RawMessage(a[0].Str))
	})
	method("error", []string{"int", "string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), x.respond("error", map[string]any{"code": a[0].Int, "message": a[1].Str})
	})
	method("abort", []string{"string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) { return lmcode.None(), errors.New(a[0].Str) })
	method("forward", []string{"string"}, "void", func(x *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if x.responded || x.forward != "" {
			return lmcode.None(), errors.New("invalid forwarding after response or second target")
		}
		x.forward = a[0].Str
		return lmcode.None(), nil
	})
	method("close", nil, "void", func(*exchange, []lmcode.Value) (lmcode.Value, error) { e.stopped = true; return lmcode.None(), nil })
	method("make_directory", []string{"string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), os.MkdirAll(a[0].Str, 0700)
	})
	method("write_stderr", []string{"string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		writer := options.Stderr
		if writer == nil {
			writer = io.Discard
		}
		n, err := io.WriteString(writer, a[0].Str)
		if err == nil && n != len(a[0].Str) {
			err = io.ErrShortWrite
		}
		return lmcode.None(), err
	})
	method("exit", []string{"int"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if a[0].Int < 0 || a[0].Int > 255 {
			return lmcode.None(), errors.New("exit code must be 0..255")
		}
		e.exitCode = int(a[0].Int)
		e.stopped = true
		return lmcode.None(), nil
	})
	method("write_text", []string{"string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), wire.writeBytes([]byte(a[0].Str))
	})
	method("emit", []string{"string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		if err := valid([]byte(a[0].Str)); err != nil {
			return lmcode.None(), err
		}
		return lmcode.None(), wire.Write([]byte(a[0].Str))
	})
	method("notify", []string{"string", "string"}, "void", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), e.write(map[string]any{"jsonrpc": "2.0", "method": a[0].Str, "params": json.RawMessage(a[1].Str)})
	})
	method("request", []string{"string", "string"}, "int", func(_ *exchange, a []lmcode.Value) (lmcode.Value, error) {
		e.nextID++
		id := strconv.FormatInt(e.nextID, 10)
		e.pending[id] = true
		err := e.write(map[string]any{"jsonrpc": "2.0", "id": e.nextID, "method": a[0].Str, "params": json.RawMessage(a[1].Str)})
		return lmcode.Int(e.nextID), err
	})
	p, err := lmcode.Load(code, Schema(), r)
	if err != nil {
		for _, name := range []string{"presets/schema-acp-v1.json", "presets/schema-v1.json"} {
			old, _ := presets.ReadFile(name)
			if previous, loadErr := lmcode.Load(code, old, r); loadErr == nil {
				p, err = previous, nil
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	e.program = p
	entries := map[string]int{}
	for i, v := range p.Entries() {
		if v.Annotation == "" || v.Iface != "interface RPCHandler(RPC) -> void" || v.Returns != "void" || len(v.Params) != 1 || v.Params[0] != "RPC" {
			return nil, errors.New("entry must implement RPCHandler(RPC) -> void")
		}
		if _, exists := entries[v.Annotation]; exists {
			return nil, errors.New("duplicate annotation")
		}
		entries[v.Annotation] = i
	}
	if err = p.Run(lmcode.NewContext(), r); err != nil {
		return nil, err
	}
	e.binding = false
	for method, annotation := range bindings {
		index, ok := entries[annotation]
		if !ok {
			return nil, fmt.Errorf("binding %s references missing annotation", method)
		}
		e.routes[method] = index
	}
	if len(e.routes) == 0 {
		return nil, errors.New("no stdio method bindings")
	}
	return e, nil
}
func (e *Engine) Stopped() bool { return e.stopped }
func (e *Engine) ExitCode() int { return e.exitCode }
func (e *Engine) write(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return e.wire.Write(b)
}
func (x *exchange) respond(key string, value any) error {
	if len(x.id) == 0 || x.responded || x.deferred {
		return errors.New("cannot reply twice or reply to a notification")
	}
	if err := x.e.write(map[string]any{"jsonrpc": "2.0", "id": x.id, key: value}); err != nil {
		return err
	}
	x.responded = true
	return nil
}
func (e *Engine) dispatch(x *exchange) error {
	target := x.method
	seen := map[string]bool{}
	for {
		if seen[target] {
			return errors.New("forwarding cycle")
		}
		seen[target] = true
		index, ok := e.routes[target]
		if !ok {
			return fmt.Errorf("unscripted method %s", target)
		}
		x.forward = ""
		_, err := e.program.Call(index, lmcode.NewContext(), e.registry, []lmcode.Value{lmcode.Obj("RPC", x)})
		if err != nil {
			return err
		}
		if x.forward != "" {
			if x.responded {
				return errors.New("forwarded handler already replied")
			}
			target = x.forward
			continue
		}
		if len(x.id) > 0 && !x.responded && !x.deferred {
			return errors.New("handler did not reply")
		}
		return nil
	}
}
func (e *Engine) Start(params any) error {
	if _, ok := e.routes["$start"]; !ok {
		return nil
	}
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return e.dispatch(&exchange{e: e, method: "$start", params: b, raw: b})
}
func (e *Engine) Run() error {
	for {
		if e.stopped {
			if len(e.pending) > 0 || len(e.deferred) > 0 {
				return errors.New("closed with pending requests")
			}
			return nil
		}
		raw, err := e.wire.Read()
		if err == io.EOF {
			if len(e.pending) > 0 || len(e.deferred) > 0 {
				return errors.New("EOF with pending client request")
			}
			return nil
		}
		if err != nil {
			return err
		}
		if err = valid(raw); err != nil {
			return errors.New("invalid JSON frame")
		}
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil || m == nil {
			return errors.New("JSON-RPC message must be an object")
		}
		if v := m["jsonrpc"]; string(v) != `"2.0"` {
			return errors.New("invalid JSON-RPC version")
		}
		id := m["id"]
		if len(id) > 0 && string(id) != "null" && id[0] != '"' && (id[0] < '0' || id[0] > '9') && id[0] != '-' {
			return errors.New("invalid RPC id")
		}
		if len(id) > 0 {
			if _, ok := e.deferred[string(id)]; ok {
				if _, isRequest := m["method"]; isRequest {
					return errors.New("inbound ID is already deferred")
				}
			}
		}
		var method string
		if v, ok := m["method"]; ok {
			if json.Unmarshal(v, &method) != nil || method == "" {
				return errors.New("invalid method")
			}
			if _, ok = m["result"]; ok {
				return errors.New("request contains result")
			}
			if _, ok = m["error"]; ok {
				return errors.New("request contains error")
			}
		}
		if p, ok := m["params"]; ok && string(p) != "null" && len(p) > 0 && p[0] != '{' && p[0] != '[' {
			return errors.New("params must be an object or array")
		}
		if method == "" {
			if len(id) == 0 || !e.pending[string(id)] {
				return errors.New("unsolicited response id")
			}
			_, result := m["result"]
			_, failure := m["error"]
			if result == failure {
				return errors.New("response requires result xor error")
			}
			delete(e.pending, string(id))
			method = "$response"
			id = nil
		}
		x := &exchange{e: e, raw: raw, method: method, params: m["params"], id: id}
		if err = e.dispatch(x); err != nil {
			if len(id) > 0 && !x.responded {
				_ = x.respond("error", map[string]any{"code": -32099, "message": err.Error()})
			}
			return err
		}
	}
}
func (e *Engine) increment(key string) (int64, error) {
	if e.opts.StateDir == "" {
		e.counts[key]++
		return e.counts[key], nil
	}
	if err := os.MkdirAll(e.opts.StateDir, 0700); err != nil {
		return 0, err
	}
	name := filepath.Join(e.opts.StateDir, fmt.Sprintf("%x", sha256.Sum256([]byte(e.opts.Alias+"\x00"+key))))
	lock, err := os.OpenFile(name+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return 0, errors.New("counter busy; concurrent same-alias turns are refused")
	}
	lock.Close()
	defer os.Remove(name + ".lock")
	n := int64(0)
	b, err := os.ReadFile(name)
	if err == nil {
		n, err = strconv.ParseInt(string(bytes.TrimSpace(b)), 10, 64)
	} else if os.IsNotExist(err) {
		err = nil
	}
	if err != nil {
		return 0, err
	}
	n++
	if n <= 0 {
		return 0, errors.New("counter overflow")
	}
	tmp, err := os.CreateTemp(e.opts.StateDir, ".counter-")
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmp.Name())
	_, err = fmt.Fprint(tmp, n)
	closeErr := tmp.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if err = os.Rename(tmp.Name(), name); err != nil {
		return 0, err
	}
	return n, nil
}
