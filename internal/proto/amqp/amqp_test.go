package amqp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	client "github.com/rabbitmq/amqp091-go"
)

func start(t *testing.T, cfg string) (string, context.CancelFunc, *server) {
	t.Helper()
	s, err := New(json.RawMessage(cfg))
	if err != nil {
		t.Fatal(err)
	}
	if s.Scheme() != "amqp" {
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
	return "amqp://guest:guest@" + ln.Addr().String() + "/", cancel, s.(*server)
}
func connect(t *testing.T, url string) *client.Connection {
	t.Helper()
	c, err := client.DialConfig(url, client.Config{Locale: "en_US", Heartbeat: time.Second, FrameSize: 4096, Dial: func(network, addr string) (net.Conn, error) {
		c, err := net.DialTimeout(network, addr, time.Second)
		if err == nil {
			c.SetDeadline(time.Now().Add(5 * time.Second))
		}
		return c, err
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func channelFor(t *testing.T, c *client.Connection) *client.Channel {
	t.Helper()
	ch, err := c.Channel()
	if err != nil {
		t.Fatal(err)
	}
	return ch
}
func declare(t *testing.T, ch *client.Channel, name string) client.Queue {
	t.Helper()
	q, err := ch.QueueDeclare(name, true, false, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func publish(t *testing.T, ch *client.Channel, q string, body []byte) {
	t.Helper()
	if err := ch.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirms := ch.NotifyPublish(make(chan client.Confirmation, 1))
	err := ch.Publish("", q, false, false, client.Publishing{Body: body, ContentType: "text/plain", DeliveryMode: 2, Headers: client.Table{"x-test": "preserved"}})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ack := <-confirms:
		if !ack.Ack {
			t.Fatal("publish rejected")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirm timeout")
	}
}
func TestFIFOAcrossConnections(t *testing.T) {
	url, _, _ := start(t, "")
	bodies := [][]byte{[]byte("first"), []byte("héllo 世界\n\x00"), bytes.Repeat([]byte("multi-frame"), 20000), {}}
	for _, body := range bodies {
		c := connect(t, url)
		ch := channelFor(t, c)
		declare(t, ch, "jobs")
		publish(t, ch, "jobs", body)
		if err := ch.Close(); err != nil {
			t.Fatal(err)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
	}
	c := connect(t, url)
	ch := channelFor(t, c)
	q := declare(t, ch, "jobs")
	if q.Messages != len(bodies) {
		t.Fatalf("queue count %d", q.Messages)
	}
	for i, want := range bodies {
		m, ok, err := ch.Get("jobs", true)
		if err != nil || !ok || !bytes.Equal(m.Body, want) {
			t.Fatalf("get %d: ok=%v err=%v", i, ok, err)
		}
		if m.MessageCount != uint32(len(bodies)-i-1) || m.DeliveryMode != 2 || m.ContentType != "text/plain" || m.Headers["x-test"] != "preserved" {
			t.Fatal("properties or remaining count lost")
		}
	}
	if _, ok, err := ch.Get("jobs", true); err != nil || ok {
		t.Fatalf("empty get: %v %v", ok, err)
	}
	// A second channel on a live connection has independent delivery/confirm state.
	ch2 := channelFor(t, c)
	declare(t, ch2, "other")
	publish(t, ch2, "other", []byte("other"))
	if _, ok, err := ch.Get("jobs", true); err != nil || ok {
		t.Fatal("queue isolation")
	}
}
func TestAuthentication(t *testing.T) {
	url, _, _ := start(t, `{"users":{"guest":"dummy-password"}}`)
	good := strings.Replace(url, "guest:guest@", "guest:dummy-password@", 1)
	c := connect(t, good)
	channelFor(t, c)
	for _, pair := range []string{"guest:wrong@", "unknown:dummy-password@"} {
		_, err := client.Dial(strings.Replace(url, "guest:guest@", pair, 1))
		if err == nil {
			t.Fatal("bad credentials accepted")
		}
		if strings.Contains(err.Error(), "dummy-password") {
			t.Fatal("password leaked")
		}
	}
}
func TestChannelErrorsAndIsolation(t *testing.T) {
	url, _, _ := start(t, "")
	c := connect(t, url)
	ch := channelFor(t, c)
	declare(t, ch, "jobs")
	_, err := ch.QueueDeclare("jobs", false, false, false, false, nil)
	var ae *client.Error
	if !errors.As(err, &ae) || ae.Code != 406 {
		t.Fatalf("declare mismatch: %v", err)
	}
	ch = channelFor(t, c)
	declare(t, ch, "jobs")
	_, _, err = ch.Get("jobs", false)
	if !errors.As(err, &ae) || ae.Code != 540 {
		t.Fatalf("manual ack: %v", err)
	}
	ch = channelFor(t, c)
	_, _, err = ch.Get("missing", true)
	if !errors.As(err, &ae) || ae.Code != 404 {
		t.Fatalf("missing queue: %v", err)
	}
	ch = channelFor(t, c)
	declare(t, ch, "jobs")
	publish(t, ch, "jobs", []byte("still alive"))
	other := connect(t, strings.TrimSuffix(url, "/")+"/other")
	oc := channelFor(t, other)
	declare(t, oc, "jobs")
	if _, ok, err := oc.Get("jobs", true); err != nil || ok {
		t.Fatal("virtual host isolation")
	}
}
func TestHeartbeatAndShutdown(t *testing.T) {
	url, cancel, _ := start(t, "")
	c := connect(t, url)
	ch := channelFor(t, c)
	time.Sleep(2200 * time.Millisecond)
	declare(t, ch, "alive")
	address := strings.Split(strings.Split(url, "@")[1], "/")[0]
	idle, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer idle.Close()
	cancel()
	idle.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := idle.Read(b[:]); err == nil {
		t.Fatal("idle handshake remained open")
	}
	select {
	case <-c.NotifyClose(make(chan *client.Error, 1)):
	case <-time.After(2 * time.Second):
		t.Fatal("active client remained open")
	}
}
func TestConfig(t *testing.T) {
	for _, cfg := range []string{`null`, `[]`, `{} {}`, `{"users":null}`, `{"users":{"u":null}}`, `{"users":{"u":1}}`, `{"unknown":"dummy-password"}`} {
		_, err := New(json.RawMessage(cfg))
		if err == nil || !strings.HasPrefix(err.Error(), "config:") || strings.Contains(err.Error(), "dummy-password") {
			t.Fatalf("bad config error: %v", err)
		}
	}
}

func TestConfirmSequenceAfterUnconfirmedPublish(t *testing.T) {
	url, _, _ := start(t, "")
	ch := channelFor(t, connect(t, url))
	declare(t, ch, "jobs")
	if err := ch.Publish("", "jobs", false, false, client.Publishing{Body: []byte("before")}); err != nil {
		t.Fatal(err)
	}
	if err := ch.Confirm(false); err != nil {
		t.Fatal(err)
	}
	confirms := ch.NotifyPublish(make(chan client.Confirmation, 3))
	for i := uint64(1); i <= 3; i++ {
		if err := ch.Publish("", "jobs", false, false, client.Publishing{Body: []byte("confirmed")}); err != nil {
			t.Fatal(err)
		}
		select {
		case ack := <-confirms:
			if !ack.Ack || ack.DeliveryTag != i {
				t.Fatalf("confirmation sequence: %+v", ack)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("confirm timeout")
		}
	}
}
