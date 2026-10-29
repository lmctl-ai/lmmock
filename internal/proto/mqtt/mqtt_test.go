package mqtt

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type client struct {
	net.Conn
	t *testing.T
}

func start(t *testing.T, cfg string) (string, context.CancelFunc) {
	t.Helper()
	s, err := New(json.RawMessage(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheme() != "mqtt" {
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
	return ln.Addr().String(), cancel
}
func dial(t *testing.T, addr string) *client {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &client{c, t}
}
func text(s string) []byte { return append([]byte{byte(len(s) >> 8), byte(len(s))}, []byte(s)...) }
func (c *client) send(header byte, body []byte) {
	c.t.Helper()
	c.SetDeadline(time.Now().Add(3 * time.Second))
	packet := []byte{header}
	n := len(body)
	for {
		b := byte(n % 128)
		n /= 128
		if n > 0 {
			b |= 128
		}
		packet = append(packet, b)
		if n == 0 {
			break
		}
	}
	packet = append(packet, body...)
	if _, err := c.Write(packet); err != nil {
		c.t.Fatal(err)
	}
}
func (c *client) read() (byte, []byte) {
	c.t.Helper()
	c.SetReadDeadline(time.Now().Add(3 * time.Second))
	b := make([]byte, 1)
	if _, err := io.ReadFull(c, b); err != nil {
		c.t.Fatal(err)
	}
	header := b[0]
	n, m := 0, 1
	for i := 0; i < 4; i++ {
		if _, err := io.ReadFull(c, b); err != nil {
			c.t.Fatal(err)
		}
		n += int(b[0]&127) * m
		if b[0]&128 == 0 {
			body := make([]byte, n)
			if _, err := io.ReadFull(c, body); err != nil {
				c.t.Fatal(err)
			}
			return header, body
		}
		m *= 128
	}
	c.t.Fatal("invalid packet length")
	return 0, nil
}
func (c *client) expect(header byte, body []byte) {
	c.t.Helper()
	h, b := c.read()
	if h != header || string(b) != string(body) {
		c.t.Fatalf("packet got %x %x want %x %x", h, b, header, body)
	}
}
func (c *client) connect(id, user, password string, want byte) {
	c.t.Helper()
	body := append(text("MQTT"), 4, 2, 0, 30)
	if user != "" {
		body[7] |= 0xc0
	}
	body = append(body, text(id)...)
	if user != "" {
		body = append(body, text(user)...)
		body = append(body, text(password)...)
	}
	c.send(0x10, body)
	c.expect(0x20, []byte{0, want})
}
func (c *client) subscribe(topic string, qos byte) {
	c.t.Helper()
	body := append([]byte{0, 1}, text(topic)...)
	body = append(body, qos)
	c.send(0x82, body)
	c.expect(0x90, []byte{0, 1, qos})
}
func (c *client) publish(topic, payload string, qos byte, retain bool) {
	c.t.Helper()
	body := text(topic)
	if qos > 0 {
		body = append(body, 0, 2)
	}
	body = append(body, []byte(payload)...)
	h := byte(0x30) | (qos << 1)
	if retain {
		h |= 1
	}
	c.send(h, body)
	if qos == 1 {
		c.expect(0x40, []byte{0, 2})
	}
	if qos == 2 {
		c.expect(0x50, []byte{0, 2})
		c.send(0x62, []byte{0, 2})
		c.expect(0x70, []byte{0, 2})
	}
}
func (c *client) receive(topic, payload string, qos byte) {
	c.t.Helper()
	h, b := c.read()
	if h&0xf0 != 0x30 || (h>>1)&3 != qos {
		c.t.Fatalf("expected PUBLISH qos %d got %x", qos, h)
	}
	n := int(binary.BigEndian.Uint16(b))
	if string(b[2:2+n]) != topic {
		c.t.Fatal("topic mismatch")
	}
	b = b[2+n:]
	var id []byte
	if qos > 0 {
		id = append([]byte{}, b[:2]...)
		b = b[2:]
	}
	if string(b) != payload {
		c.t.Fatal("payload mismatch")
	}
	if qos == 1 {
		c.send(0x40, id)
	}
	if qos == 2 {
		c.send(0x50, id)
		c.expect(0x62, id)
		c.send(0x70, id)
	}
}
func TestRoundTrip(t *testing.T) {
	addr, _ := start(t, "")
	sub := dial(t, addr)
	sub.connect("sub", "", "", 0)
	sub.subscribe("demo/+", 2)
	pub := dial(t, addr)
	pub.connect("pub", "", "", 0)
	for qos := byte(0); qos <= 2; qos++ {
		payload := fmt.Sprintf("héllo 世界\n%v\x00", qos)
		pub.publish("demo/topic", payload, qos, false)
		sub.receive("demo/topic", payload, qos)
	}
	pub.send(0xc0, nil)
	pub.expect(0xd0, nil)
	// Subscription acknowledgment is the synchronization point; no arbitrary sleeps.
	body := append([]byte{0, 3}, text("demo/+")...)
	sub.send(0xa2, body)
	sub.expect(0xb0, []byte{0, 3})
	sub.send(0xe0, nil)
	pub.send(0xe0, nil)
}
func TestRetainedAndAuth(t *testing.T) {
	addr, _ := start(t, `{"users":{"u":"dummy-password"}}`)
	dial(t, addr).connect("anonymous", "", "", 5)
	dial(t, addr).connect("wrong", "u", "wrong", 5)
	dial(t, addr).connect("unknown", "x", "dummy-password", 5)
	pub := dial(t, addr)
	pub.connect("pub", "u", "dummy-password", 0)
	pub.publish("retained", "value", 1, true)
	sub := dial(t, addr)
	sub.connect("sub", "u", "dummy-password", 0)
	sub.subscribe("retained", 1)
	sub.receive("retained", "value", 1)
}
func TestShutdownClosesUnfinishedHandshake(t *testing.T) {
	addr, cancel := start(t, "")
	idle := dial(t, addr)
	connected := dial(t, addr)
	connected.connect("live", "", "", 0)
	cancel()
	for _, c := range []*client{idle, connected} {
		c.SetReadDeadline(time.Now().Add(time.Second))
		b := make([]byte, 128)
		for {
			_, err := c.Read(b)
			if err != nil {
				if e, ok := err.(net.Error); ok && e.Timeout() {
					t.Error("connection not closed")
				}
				break
			}
		}
	}
}
func TestConfig(t *testing.T) {
	for _, cfg := range []string{`null`, `[]`, `{"users":null}`, `{"users":{"u":null}}`, `{"users":"dummy-secret"}`, `{"users":{"u":1}}`, `{"unknown":"dummy-secret"}`, `{} {}`} {
		if _, err := New(json.RawMessage(cfg)); err == nil || !strings.HasPrefix(err.Error(), "config:") || strings.Contains(err.Error(), "dummy-secret") {
			t.Fatalf("invalid config handling: %v", err)
		}
	}
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
