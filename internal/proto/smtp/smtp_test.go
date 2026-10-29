package smtp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/emersion/go-sasl"
	wire "github.com/emersion/go-smtp"
	"github.com/lmctl-ai/lmmock/internal/proto/mailstore"
)

func start(t *testing.T, cfg string) (*Server, string, context.CancelFunc) {
	t.Helper()
	p, err := New(json.RawMessage(cfg))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { done <- p.Serve(ctx, ln); close(stopped) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return p.(*Server), ln.Addr().String(), func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("cancellation timed out")
		}
	}
}
func client(t *testing.T, address string) *wire.Client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	c := wire.NewClient(conn)
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestDeliveryAndAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.jsonl")
	cfg, _ := json.Marshal(map[string]any{"users": map[string]string{"test": "secret"}, "spool": path})
	s, address, _ := start(t, string(cfg))
	raw := []byte("From: a@example.test\r\nTo: b@example.test\r\nSubject: fixture\r\n\r\nline\r\n.dot\r\n")
	for _, auth := range []sasl.Client{nil, sasl.NewPlainClient("", "test", "secret"), sasl.NewLoginClient("test", "secret")} {
		c := client(t, address)
		if auth != nil {
			if err := c.Auth(auth); err != nil {
				t.Fatal(err)
			}
		}
		if err := c.SendMail("a@example.test", []string{"b@example.test", "c@example.test"}, bytes.NewReader(raw)); err != nil {
			t.Fatal(err)
		}
		if err := c.Quit(); err != nil {
			t.Fatal(err)
		}
	}
	messages := s.Messages()
	spool, err := mailstore.Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || len(spool) != 3 {
		t.Fatal("wrong stored count")
	}
	for _, m := range spool {
		if !bytes.Equal(m.Data, raw) || len(m.To) != 2 || m.From != "a@example.test" {
			t.Fatal("message/envelope changed")
		}
	}
	for _, auth := range []sasl.Client{sasl.NewPlainClient("", "test", "wrong"), sasl.NewLoginClient("unknown", "secret")} {
		err := client(t, address).Auth(auth)
		var e *wire.SMTPError
		if !errors.As(err, &e) || e.Code != 535 {
			t.Fatalf("expected 535, got %v", err)
		}
	}
}
func TestDefaultAuthAndCancellation(t *testing.T) {
	_, addr, cancel := start(t, "")
	c := client(t, addr)
	if err := c.Auth(sasl.NewLoginClient("any", "dummy")); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := c.Noop(); err == nil {
		t.Fatal("connection survived cancellation")
	}
}
func TestInvalidConfig(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `{"users":1}`, `{"unexpected":true}`, `{} {}`} {
		if _, err := New(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
