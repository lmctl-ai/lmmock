package redis

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

type client struct {
	net.Conn
	r *bufio.Reader
	t *testing.T
}

func start(t *testing.T, cfg string) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	s, err := New(json.RawMessage(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheme() != "redis" {
		t.Fatal("scheme")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("server did not stop")
		}
	})
	return ln.Addr().String(), cancel, done
}
func dial(t *testing.T, addr string) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &client{Conn: c, r: bufio.NewReader(c), t: t}
}
func (c *client) send(args ...string) {
	c.t.Helper()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(a), a)
	}
	if _, err := io.WriteString(c.Conn, b.String()); err != nil {
		c.t.Fatal(err)
	}
}
func (c *client) read() any {
	c.t.Helper()
	line, err := c.r.ReadString('\n')
	if err != nil {
		c.t.Fatal(err)
	}
	line = strings.TrimSuffix(line, "\r\n")
	switch line[0] {
	case '+':
		return line[1:]
	case '-':
		return line
	case ':':
		n, _ := strconv.Atoi(line[1:])
		return n
	case '$':
		n, _ := strconv.Atoi(line[1:])
		if n == -1 {
			return nil
		}
		b := make([]byte, n+2)
		if _, err := io.ReadFull(c.r, b); err != nil {
			c.t.Fatal(err)
		}
		return string(b[:n])
	case '*':
		n, _ := strconv.Atoi(line[1:])
		v := make([]any, n)
		for i := range v {
			v[i] = c.read()
		}
		return v
	}
	c.t.Fatalf("bad RESP frame")
	return nil
}
func (c *client) check(want any, args ...string) {
	c.t.Helper()
	c.send(args...)
	if got := c.read(); !reflect.DeepEqual(got, want) {
		c.t.Fatalf("%s: got %#v want %#v", args[0], got, want)
	}
}
func TestCommands(t *testing.T) {
	addr, _, _ := start(t, "")
	c := dial(t, addr)
	c.check("PONG", "PING")
	c.check("echo", "PING", "echo")
	c.check("OK", "CLIENT", "SETINFO", "LIB-NAME", "jedis")
	c.check("OK", "CLIENT", "SETNAME", "test")
	c.check(nil, "GET", "missing")
	v := "héllo 世界\n\x00"
	c.check("OK", "SET", "key", v)
	c.check(v, "GET", "key")
	c.check("OK", "SELECT", "1")
	c.check(nil, "GET", "key")
	c.check("OK", "SET", "key", "other")
	c.check("OK", "SELECT", "0")
	c.check(v, "GET", "key")
	c.check(1, "DEL", "key", "missing", "key")
	c.check(0, "DEL", "key")
	c.check("OK", "SET", "exp", "gone", "EX", "1")
	time.Sleep(1100 * time.Millisecond)
	c.check(nil, "GET", "exp")
	c.check(nil, "RPOP", "list")
	c.check(3, "LPUSH", "list", "one", "two", "三")
	c.check([]any{"三", "two", "one"}, "LRANGE", "list", "0", "-1")
	c.check([]any{"two"}, "LRANGE", "list", "-2", "-2")
	c.check([]any{}, "LRANGE", "list", "10", "20")
	c.check([]any{}, "LRANGE", "list", "0", "-100")
	c.check("-WRONGTYPE Operation against a key holding the wrong kind of value", "GET", "list")
	c.check("one", "RPOP", "list")
	c.check("two", "RPOP", "list")
	c.check("三", "RPOP", "list")
	c.check(nil, "RPOP", "list")
	c.check("OK", "SET", "list", "string")
	for _, args := range [][]string{{"LPUSH", "list", "x"}, {"RPOP", "list"}, {"LRANGE", "list", "0", "1"}} {
		c.check("-WRONGTYPE Operation against a key holding the wrong kind of value", args...)
	}
	c.check("OK", "REPLICAOF", "127.0.0.1", "1")
	c.check("-READONLY You can't write against a read only replica", "SET", "x", "y")
	c.check("OK", "REPLICAOF", "NO", "ONE")
	c.check("OK", "SET", "x", "y")
	c.check("-ERR DB index is out of range", "SELECT", "16")
	c.check("-ERR invalid expire time", "SET", "k", "v", "EX", "0")
	c.check("-ERR invalid expire time", "SET", "k", "v", "EX", "9223372036854775807")
	c.check("-ERR value is not an integer or out of range", "LRANGE", "x", "bad", "0")
	c.check("-ERR unsupported command", "BOGUS")
	for _, op := range []string{"GET", "SET", "DEL", "LPUSH", "RPOP", "LRANGE", "SELECT", "PUBLISH", "SUBSCRIBE", "REPLICAOF", "AUTH"} {
		c.send(op)
		if got := c.read(); !strings.HasPrefix(fmt.Sprint(got), "-ERR") {
			t.Fatalf("%s missing args: %v", op, got)
		}
	}
	c.check("OK", "QUIT")
}
func TestAuth(t *testing.T) {
	addr, _, _ := start(t, `{"requirepass":"dummy-secret"}`)
	c := dial(t, addr)
	c.check("-NOAUTH Authentication required", "PING")
	c.check("-WRONGPASS invalid username-password pair", "AUTH", "wrong")
	c.check("OK", "AUTH", "dummy-secret")
	c.check("PONG", "PING")
	c.check("-WRONGPASS invalid username-password pair", "AUTH", "other", "dummy-secret")
	c.check("-NOAUTH Authentication required", "PING")
	c.check("OK", "AUTH", "default", "dummy-secret")
	c.check("PONG", "PING")
	other := dial(t, addr)
	other.check("-NOAUTH Authentication required", "GET", "x")
}
func TestPubSubAndShutdown(t *testing.T) {
	addr, cancel, _ := start(t, "")
	sub := dial(t, addr)
	pub := dial(t, addr)
	idle := dial(t, addr)
	pub.check(0, "PUBLISH", "news", "none")
	sub.check([]any{"subscribe", "news", 1}, "SUBSCRIBE", "news")
	pub.check(1, "PUBLISH", "news", "hello")
	if got := sub.read(); !reflect.DeepEqual(got, []any{"message", "news", "hello"}) {
		t.Fatal(got)
	}
	cancel()
	for _, c := range []*client{sub, pub, idle} {
		c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := c.r.ReadByte(); err == nil {
			t.Error("connection not closed")
		}
	}
}
func TestConfig(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{"requirepass":null}`, `{"readonly":null}`, `{"unknown":"dummy-secret"}`, `{"requirepass":1}`, `{} {}`, `{"readonly":"yes"}`} {
		if _, err := New(json.RawMessage(raw)); err == nil || !strings.HasPrefix(err.Error(), "config:") || strings.Contains(err.Error(), "dummy-secret") {
			t.Fatalf("bad config handling: %v", err)
		}
	}
	addr, _, _ := start(t, `{"readonly":true}`)
	dial(t, addr).check("-READONLY You can't write against a read only replica", "SET", "a", "b")
}

// Test the address guard without exposing an actual non-loopback socket.
type nonLoopbackListener struct{ net.Listener }

func (l nonLoopbackListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}
}
func TestRejectNonLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Serve(context.Background(), nonLoopbackListener{ln}); err == nil {
		t.Fatal("accepted non-loopback listener")
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		c.Close()
		t.Fatal("listener not closed on rejection")
	}
}
