// Package line is the worked extension: LF framing only, with lmcode policy.
package line

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"io"
	"net"
	"strings"
	"sync"
)

func init() {
	wire.Register("line", wire.Definition{SAM: "LineHandler", New: func(_ string, raw json.RawMessage) (wire.Layer, error) {
		var options struct{}
		if err := wire.Decode(raw, &options); err != nil {
			return nil, err
		}
		return Layer{}, nil
	}})
}

type Layer struct{}

func (Layer) Scheme() string { return "tcp" }
func (Layer) ValidateRoute(key string) error {
	if key != "connection" {
		return errors.New("line binding must be connection")
	}
	return nil
}
func (Layer) Serve(ctx context.Context, ln net.Listener, e *wire.Engine) error {
	var mu sync.Mutex
	var work sync.WaitGroup
	conns := map[net.Conn]bool{}
	closed := false
	closeAll := func() {
		mu.Lock()
		closed = true
		_ = ln.Close()
		for c := range conns {
			_ = c.Close()
		}
		mu.Unlock()
	}
	stop := context.AfterFunc(ctx, closeAll)
	defer stop()
	defer func() { closeAll(); work.Wait() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		mu.Lock()
		if closed {
			mu.Unlock()
			_ = c.Close()
			continue
		}
		conns[c] = true
		work.Add(1)
		mu.Unlock()
		go func() {
			defer work.Done()
			defer c.Close()
			defer func() { mu.Lock(); delete(conns, c); mu.Unlock() }()
			_ = e.Dispatch("connection", &session{conn: c, reader: bufio.NewReader(c)})
		}()
	}
}

type session struct {
	wire.BaseSession
	conn   net.Conn
	reader *bufio.Reader
}

func (s *session) Receive() (string, string, error) {
	text, err := s.reader.ReadString('\n')
	if err != nil {
		return "", "", err
	}
	return "line", strings.TrimSuffix(text, "\n"), nil
}
func (s *session) Send(kind, body string) error {
	if kind != "" || strings.ContainsAny(body, "\r\n") {
		return errors.New("line send requires one LF-free text line")
	}
	_, err := io.WriteString(s.conn, body+"\n")
	return err
}
