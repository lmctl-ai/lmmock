package rpcstdio

import (
	"encoding/json"
	"errors"
	lmcode "github.com/lmctl-ai/lmcode-go"
)

// Deferred replies are protocol correlation, independent of any method dialect.
// A handler retains its inbound ID, yields to the input loop, then an event
// handler resolves it. No goroutine, timer, polling or provider policy is needed.
func (e *Engine) registerDeferred() error {
	if err := e.registry.Method("RPC", "defer_reply", nil, "string", func(self any, _ []lmcode.Value) (lmcode.Value, error) {
		x := self.(*exchange)
		if len(x.id) == 0 || x.responded || x.deferred {
			return lmcode.None(), errors.New("cannot defer a notification or completed request")
		}
		key := string(x.id)
		if _, ok := e.deferred[key]; ok {
			return lmcode.None(), errors.New("duplicate deferred request ID")
		}
		e.deferred[key] = append(json.RawMessage(nil), x.id...)
		x.deferred = true
		return lmcode.String(key), nil
	}); err != nil {
		return err
	}
	return e.registry.Method("RPC", "reply_to", []string{"string", "string"}, "void", func(_ any, a []lmcode.Value) (lmcode.Value, error) {
		key := a[0].Str
		id, ok := e.deferred[key]
		if !ok {
			return lmcode.None(), errors.New("unknown or completed deferred request")
		}
		if err := valid([]byte(a[1].Str)); err != nil {
			return lmcode.None(), err
		}
		if err := e.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": json.RawMessage(a[1].Str)}); err != nil {
			return lmcode.None(), err
		}
		delete(e.deferred, key)
		return lmcode.None(), nil
	})
}
