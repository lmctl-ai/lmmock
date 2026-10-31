package rpcstdio

import "io"

// RunJSONLines dispatches complete JSON values without imposing JSON-RPC
// envelopes. Framing and checked writes are shared; protocol policy lives in
// annotated $start/$message handlers, including whether to close after a record.
func (e *Engine) RunJSONLines() error {
	for !e.stopped {
		raw, err := e.wire.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if err = valid(raw); err != nil {
			return err
		}
		if err = e.dispatch(&exchange{e: e, raw: raw, params: raw, method: "$message"}); err != nil {
			return err
		}
	}
	return nil
}
