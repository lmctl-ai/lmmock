// Package redis implements the lmclient RESP2 command subset in memory:
// PING, AUTH, SELECT (databases 0..15), CLIENT SETINFO/SETNAME, SET [EX seconds],
// GET, DEL, LPUSH, RPOP, LRANGE, SUBSCRIBE, PUBLISH, REPLICAOF, and QUIT.
// Config: {"requirepass":"dummy","readonly":false}. All fields are optional;
// unknown fields/types are errors. AUTH accepts a password or default/password.
// REPLICAOF host port enables simulated READONLY errors; REPLICAOF NO ONE clears
// them. There is no replication, persistence, TLS, cluster, or RESP3 support.
// Connections have a 60-second idle read limit and 5-second write limit.
package redis

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
	"github.com/tidwall/redcon"
)

type config struct {
	Requirepass string `json:"requirepass"`
	Readonly    bool   `json:"readonly"`
}
type entry struct {
	text    string
	list    []string
	isList  bool
	expires time.Time
}
type session struct {
	authenticated bool
	db            int
}
type server struct {
	cfg      config
	mu       sync.Mutex
	dbs      [16]map[string]*entry
	readonly bool
	pubsub   redcon.PubSub
}

func init() { proto.Register("redis", New) }
func New(raw json.RawMessage) (proto.Server, error) {
	var cfg config
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || dec.Decode(&cfg) != nil {
			return nil, proto.ConfigError("redis expects an object with requirepass (string) and readonly (boolean)")
		}
		// encoding/json accepts null for scalar/map fields; the config schema does not.
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		for _, field := range fields {
			if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
				return nil, proto.ConfigError("redis config fields must not be null")
			}
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, proto.ConfigError("redis expects one configuration object")
		}
	}
	s := &server{cfg: cfg, readonly: cfg.Readonly}
	for i := range s.dbs {
		s.dbs[i] = map[string]*entry{}
	}
	return s, nil
}
func (*server) Scheme() string { return "redis" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return errors.New("redis requires a loopback TCP listener")
	}
	tracked := &trackedListener{Listener: ln, connections: map[*boundedConn]bool{}}
	defer tracked.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			tracked.Close()
		case <-done:
		}
	}()
	rs := redcon.NewServer("", s.command, func(c redcon.Conn) bool { c.SetContext(&session{authenticated: s.cfg.Requirepass == ""}); return true }, nil)
	rs.SetIdleClose(60 * time.Second)
	err := rs.Serve(tracked)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// Keep detached redcon pub/sub connections in the shutdown set too.
type trackedListener struct {
	net.Listener
	mu          sync.Mutex
	closed      bool
	connections map[*boundedConn]bool
}
type boundedConn struct {
	net.Conn
	owner *trackedListener
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	b := &boundedConn{Conn: c, owner: l}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		c.Close()
		return nil, net.ErrClosed
	}
	l.connections[b] = true
	return b, nil
}
func (l *trackedListener) Close() error {
	err := l.Listener.Close()
	l.mu.Lock()
	l.closed = true
	for c := range l.connections {
		c.Conn.Close()
	}
	l.mu.Unlock()
	return err
}
func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.owner.mu.Lock()
	delete(c.owner.connections, c)
	c.owner.mu.Unlock()
	return err
}
func (c *boundedConn) Read(p []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Read(p)
}
func (c *boundedConn) Write(p []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.Conn.Write(p)
}
func (s *server) get(db int, key string) *entry {
	e := s.dbs[db][key]
	if e != nil && !e.expires.IsZero() && !time.Now().Before(e.expires) {
		delete(s.dbs[db], key)
		return nil
	}
	return e
}
func (s *server) command(c redcon.Conn, cmd redcon.Command) {
	if len(cmd.Args) == 0 {
		return
	}
	a := cmd.Args
	op := strings.ToUpper(string(a[0]))
	st := c.Context().(*session)
	arity := func(n int) bool {
		if len(a) != n {
			c.WriteError("ERR wrong number of arguments")
			return false
		}
		return true
	}
	if op == "AUTH" {
		if len(a) != 2 && len(a) != 3 {
			c.WriteError("ERR wrong number of arguments")
			return
		}
		valid := s.cfg.Requirepass != "" && subtle.ConstantTimeCompare(a[len(a)-1], []byte(s.cfg.Requirepass)) == 1 && (len(a) == 2 || string(a[1]) == "default")
		st.authenticated = valid
		if !valid {
			c.WriteError("WRONGPASS invalid username-password pair")
		} else {
			c.WriteString("OK")
		}
		return
	}
	if !st.authenticated {
		c.WriteError("NOAUTH Authentication required")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.readonly && (op == "SET" || op == "DEL" || op == "LPUSH" || op == "RPOP") {
		c.WriteError("READONLY You can't write against a read only replica")
		return
	}
	switch op {
	case "PING":
		if len(a) == 1 {
			c.WriteString("PONG")
		} else if arity(2) {
			c.WriteBulk(a[1])
		}
	case "QUIT":
		if arity(1) {
			c.WriteString("OK")
			c.Close()
		}
	case "CLIENT":
		if len(a) == 4 && strings.EqualFold(string(a[1]), "SETINFO") || len(a) == 3 && strings.EqualFold(string(a[1]), "SETNAME") {
			c.WriteString("OK")
		} else {
			c.WriteError("ERR unsupported CLIENT command")
		}
	case "SELECT":
		if !arity(2) {
			return
		}
		n, err := strconv.Atoi(string(a[1]))
		if err != nil || n < 0 || n >= len(s.dbs) {
			c.WriteError("ERR DB index is out of range")
			return
		}
		st.db = n
		c.WriteString("OK")
	case "SET":
		if len(a) != 3 && len(a) != 5 {
			c.WriteError("ERR syntax error")
			return
		}
		e := &entry{text: string(a[2])}
		if len(a) == 5 {
			n, err := strconv.ParseInt(string(a[4]), 10, 64)
			if !strings.EqualFold(string(a[3]), "EX") || err != nil || n <= 0 || n > int64((1<<63-1)/time.Second) {
				c.WriteError("ERR invalid expire time")
				return
			}
			e.expires = time.Now().Add(time.Duration(n) * time.Second)
		}
		s.dbs[st.db][string(a[1])] = e
		c.WriteString("OK")
	case "GET":
		if !arity(2) {
			return
		}
		e := s.get(st.db, string(a[1]))
		if e == nil {
			c.WriteNull()
		} else if e.isList {
			c.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
		} else {
			c.WriteBulkString(e.text)
		}
	case "DEL":
		if len(a) < 2 {
			c.WriteError("ERR wrong number of arguments")
			return
		}
		n := 0
		for _, key := range a[1:] {
			if s.get(st.db, string(key)) != nil {
				delete(s.dbs[st.db], string(key))
				n++
			}
		}
		c.WriteInt(n)
	case "LPUSH":
		if len(a) < 3 {
			c.WriteError("ERR wrong number of arguments")
			return
		}
		key := string(a[1])
		e := s.get(st.db, key)
		if e == nil {
			e = &entry{isList: true}
			s.dbs[st.db][key] = e
		}
		if !e.isList {
			c.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
			return
		}
		for _, v := range a[2:] {
			e.list = append([]string{string(v)}, e.list...)
		}
		c.WriteInt(len(e.list))
	case "RPOP":
		if !arity(2) {
			return
		}
		key := string(a[1])
		e := s.get(st.db, key)
		if e == nil {
			c.WriteNull()
			return
		}
		if !e.isList {
			c.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
			return
		}
		n := len(e.list) - 1
		c.WriteBulkString(e.list[n])
		e.list = e.list[:n]
		if n == 0 {
			delete(s.dbs[st.db], key)
		}
	case "LRANGE":
		if !arity(4) {
			return
		}
		start, err := strconv.ParseInt(string(a[2]), 10, 64)
		stop, err2 := strconv.ParseInt(string(a[3]), 10, 64)
		if err != nil || err2 != nil {
			c.WriteError("ERR value is not an integer or out of range")
			return
		}
		e := s.get(st.db, string(a[1]))
		if e == nil {
			c.WriteArray(0)
			return
		}
		if !e.isList {
			c.WriteError("WRONGTYPE Operation against a key holding the wrong kind of value")
			return
		}
		n := int64(len(e.list))
		if start < 0 {
			start += n
		}
		if stop < 0 {
			stop += n
		}
		if start < 0 {
			start = 0
		}
		if stop >= n {
			stop = n - 1
		}
		if start > stop || start >= n || stop < 0 {
			c.WriteArray(0)
			return
		}
		c.WriteArray(int(stop - start + 1))
		for _, v := range e.list[start : stop+1] {
			c.WriteBulkString(v)
		}
	case "SUBSCRIBE":
		if len(a) < 2 {
			c.WriteError("ERR wrong number of arguments")
			return
		}
		for _, channel := range a[1:] {
			s.pubsub.Subscribe(c, string(channel))
		}
	case "PUBLISH":
		if arity(3) {
			c.WriteInt(s.pubsub.Publish(string(a[1]), string(a[2])))
		}
	case "REPLICAOF":
		if !arity(3) {
			return
		}
		if strings.EqualFold(string(a[1]), "NO") && strings.EqualFold(string(a[2]), "ONE") {
			s.readonly = false
		} else {
			port, err := strconv.Atoi(string(a[2]))
			if err != nil || port < 1 || port > 65535 {
				c.WriteError("ERR invalid port")
				return
			}
			s.readonly = true
		}
		c.WriteString("OK")
	default:
		c.WriteError("ERR unsupported command")
	}
}
