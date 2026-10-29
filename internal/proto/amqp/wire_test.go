package amqp

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

type rawClient struct {
	net.Conn
	t *testing.T
}

func rawDial(t *testing.T, url string) *rawClient {
	t.Helper()
	address := strings.Split(strings.Split(url, "@")[1], "/")[0]
	c, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return &rawClient{c, t}
}
func (r *rawClient) write(f frame) {
	r.t.Helper()
	r.SetWriteDeadline(time.Now().Add(time.Second))
	if err := writeFrame(r, f); err != nil {
		r.t.Fatal(err)
	}
}
func (r *rawClient) read() frame {
	r.t.Helper()
	r.SetReadDeadline(time.Now().Add(2 * time.Second))
	f, err := readFrame(r, frameLimit)
	if err != nil {
		r.t.Fatal(err)
	}
	return f
}
func (r *rawClient) method(ch, class, method uint16, args encoder) {
	r.t.Helper()
	var b encoder
	b.u16(class)
	b.u16(method)
	b = append(b, args...)
	r.write(frame{1, ch, b})
}
func (r *rawClient) expect(ch, class, method uint16) decoder {
	r.t.Helper()
	f := r.read()
	d := decoder{p: f.payload}
	if f.kind != 1 || f.channel != ch || d.u16() != class || d.u16() != method || d.bad {
		r.t.Fatalf("unexpected response %v", f)
	}
	return d
}
func (r *rawClient) start(user, password string) {
	r.t.Helper()
	r.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := io.WriteString(r, "AMQP\x00\x00\x09\x01"); err != nil {
		r.t.Fatal(err)
	}
	r.expect(0, 10, 10)
	var b encoder
	b.long(nil)
	b.short("PLAIN")
	b.long([]byte("\x00" + user + "\x00" + password))
	b.short("en_US")
	r.method(0, 10, 11, b)
}
func rawReady(t *testing.T, url string) *rawClient {
	t.Helper()
	r := rawDial(t, url)
	r.start("guest", "guest")
	r.expect(0, 10, 30)
	var tune encoder
	tune.u16(128)
	tune.u32(4096)
	tune.u16(0)
	r.method(0, 10, 31, tune)
	var open encoder
	open.short("/")
	open.short("")
	open.u8(0)
	r.method(0, 10, 40, open)
	r.expect(0, 10, 41)
	var ch encoder
	ch.short("")
	r.method(1, 20, 10, ch)
	r.expect(1, 20, 11)
	var declare encoder
	declare.u16(0)
	declare.short("raw")
	declare.u8(2)
	declare.long(nil)
	r.method(1, 50, 10, declare)
	r.expect(1, 50, 11)
	r.method(1, 85, 10, encoder{0})
	r.expect(1, 85, 11)
	return r
}
func (r *rawClient) publishHeader(size uint64) {
	var publish encoder
	publish.u16(0)
	publish.short("")
	publish.short("raw")
	publish.u8(0)
	r.method(1, 60, 40, publish)
	var header encoder
	header.u16(60)
	header.u16(0)
	header.u64(size)
	header.u16(0)
	r.write(frame{2, 1, header})
}
func TestConfirmOnlyAfterCompleteBody(t *testing.T) {
	url, _, s := start(t, "")
	r := rawReady(t, url)
	observer := channelFor(t, connect(t, url))
	r.publishHeader(6)
	r.write(frame{3, 1, []byte("abc")})
	r.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	if _, err := readFrame(r, 4096); err == nil {
		t.Fatal("confirm before complete body")
	} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatal(err)
	}
	if _, ok, err := observer.Get("raw", true); err != nil || ok {
		t.Fatal("incomplete publish exposed")
	}
	r.write(frame{3, 1, []byte("def")})
	ack := r.expect(1, 60, 80)
	if ack.u64() != 1 || ack.u8() != 0 || !ack.done() {
		t.Fatal("bad confirmation")
	}
	m, ok, err := observer.Get("raw", true)
	if err != nil || !ok || string(m.Body) != "abcdef" {
		t.Fatal("body assembly failed")
	}
	r.publishHeader(1000)
	r.write(frame{3, 1, []byte("partial")})
	// Opening another channel is a barrier proving the partial body was processed.
	var opened encoder
	opened.short("")
	r.method(2, 20, 10, opened)
	r.expect(2, 20, 11)
	s.mu.Lock()
	reserved := s.inflight
	s.mu.Unlock()
	if reserved != 1002 {
		t.Fatalf("expected reserved partial body, got %d", reserved)
	}
	r.Close()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		n := s.inflight
		s.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("partial publish reservation leaked")
		}
		time.Sleep(time.Millisecond)
	}
	if _, ok, err := observer.Get("raw", true); err != nil || ok {
		t.Fatal("disconnected partial publish stored")
	}
}
func TestRawAuthCodeAndProtocolHeader(t *testing.T) {
	url, _, _ := start(t, `{"users":{"u":"dummy-password"}}`)
	r := rawDial(t, url)
	r.start("u", "wrong")
	d := r.expect(0, 10, 50)
	if d.u16() != 403 || !strings.HasPrefix(d.short(), "ACCESS_REFUSED") {
		t.Fatal("missing ACCESS_REFUSED")
	}
	r.method(0, 10, 51, nil)
	r = rawDial(t, url)
	r.SetDeadline(time.Now().Add(time.Second))
	r.Write([]byte("AMQP\x00\x01\x00\x00"))
	var supported [8]byte
	if _, err := io.ReadFull(r, supported[:]); err != nil || string(supported[:]) != "AMQP\x00\x00\x09\x01" {
		t.Fatal("protocol negotiation", err)
	}
}
func TestRejectOversizedBodyAndBadSequence(t *testing.T) {
	url, _, _ := start(t, "")
	r := rawReady(t, url)
	r.publishHeader(bodyLimit + 1)
	d := r.expect(1, 20, 40)
	if d.u16() != 311 {
		t.Fatal("body limit")
	}
	r.method(1, 20, 41, nil)
	r = rawReady(t, url)
	r.write(frame{3, 1, []byte("no publish")})
	d = r.expect(0, 10, 50)
	if d.u16() != 505 {
		t.Fatal("body without publish")
	}
	r.method(0, 10, 51, nil)
	r = rawReady(t, url)
	header := []byte{3, 0, 1, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(header[3:], 0xffffffff)
	r.Write(header)
	d = r.expect(0, 10, 50)
	if d.u16() != 501 {
		t.Fatal("frame limit")
	}
	r.method(0, 10, 51, nil)
}
func TestReadFrameBounds(t *testing.T) {
	for _, b := range [][]byte{{1, 0, 0, 255, 255, 255, 255}, {8, 0, 1, 0, 0, 0, 0, 0xce}, {8, 0, 0, 0, 0, 0, 1, 0, 0xce}, {1, 0, 0, 0, 0, 0, 0, 0}, {9, 0, 0, 0, 0, 0, 0, 0xce}} {
		if _, err := readFrame(bytes.NewReader(b), 4096); err == nil {
			t.Fatal("invalid frame accepted")
		}
	}
}

type nonLoopbackListener struct{ net.Listener }

func (nonLoopbackListener) Addr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 1234}
}

func TestRejectNonLoopback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	s, _ := New(nil)
	if err := s.Serve(context.Background(), nonLoopbackListener{ln}); err == nil {
		t.Fatal("non-loopback accepted")
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		c.Close()
		t.Fatal("listener not closed")
	}
}
