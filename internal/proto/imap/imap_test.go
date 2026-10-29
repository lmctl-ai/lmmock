package imap

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wire "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	smtpclient "github.com/emersion/go-smtp"
	"github.com/lmctl-ai/lmmock/internal/proto"
	"github.com/lmctl-ai/lmmock/internal/proto/mailstore"
	smtpserver "github.com/lmctl-ai/lmmock/internal/proto/smtp"
)

func serve(t *testing.T, s proto.Server) (string, context.CancelFunc) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	stopped := make(chan struct{})
	go func() { done <- s.Serve(ctx, ln); close(stopped) }()
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
	return ln.Addr().String(), func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("cancellation timed out")
		}
	}
}
func client(t *testing.T, address string) *imapclient.Client {
	t.Helper()
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	c := imapclient.New(conn, nil)
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func TestSeededMailbox(t *testing.T) {
	cfg := mailstore.Config{Users: map[string]string{"test": "secret"}, Messages: []mailstore.Seed{{From: "sender@example.test", To: []string{"test@localhost"}, Subject: "café 世界", Body: "line\nsecond", Date: "2024-01-02T03:04:05Z"}}}
	raw, _ := json.Marshal(cfg)
	s, err := New(raw)
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := serve(t, s)
	c := client(t, addr)
	if err := c.Login("test", "wrong").Wait(); err == nil {
		t.Fatal("bad password accepted")
	}
	if err := c.Login("test", "secret").Wait(); err != nil {
		t.Fatal(err)
	}
	list, err := c.List("", "*", nil).Collect()
	if err != nil || len(list) != 1 || list[0].Mailbox != "INBOX" {
		t.Fatalf("list: %v %v", list, err)
	}
	status, err := c.Status("INBOX", &wire.StatusOptions{NumMessages: true, NumUnseen: true}).Wait()
	if err != nil || *status.NumMessages != 1 || *status.NumUnseen != 1 {
		t.Fatalf("status: %v %v", status, err)
	}
	selected, err := c.Select("inbox", &wire.SelectOptions{ReadOnly: true}).Wait()
	if err != nil || selected.NumMessages != 1 {
		t.Fatalf("select: %v %v", selected, err)
	}
	section := &wire.FetchItemBodySection{Peek: true}
	fetched, err := c.Fetch(wire.SeqSetNum(1), &wire.FetchOptions{Envelope: true, UID: true, BodySection: []*wire.FetchItemBodySection{section}}).Collect()
	if err != nil || len(fetched) != 1 {
		t.Fatalf("fetch: %v", err)
	}
	expected, _ := cfg.Messages[0].Message()
	if !bytes.Equal(fetched[0].FindBodySection(section), expected.Data) {
		t.Fatal("seed bytes changed")
	}
	if fetched[0].Envelope.Subject != "café 世界" {
		t.Fatalf("subject: %q", fetched[0].Envelope.Subject)
	}
	unseen, err := c.Search(&wire.SearchCriteria{NotFlag: []wire.Flag{wire.FlagSeen}}, nil).Wait()
	if err != nil || len(unseen.AllSeqNums()) != 1 {
		t.Fatalf("peek marked message seen: %v", err)
	}
	uid := fetched[0].UID
	uids, err := c.UIDSearch(&wire.SearchCriteria{}, nil).Wait()
	if err != nil || len(uids.AllUIDs()) != 1 || uids.AllUIDs()[0] != uid {
		t.Fatalf("UID SEARCH: %v", err)
	}
	header := &wire.FetchItemBodySection{Specifier: wire.PartSpecifierHeader, Peek: true}
	fetched, err = c.Fetch(wire.UIDSetNum(uid), &wire.FetchOptions{BodySection: []*wire.FetchItemBodySection{header}}).Collect()
	if err != nil || len(fetched) != 1 || !bytes.Contains(fetched[0].FindBodySection(header), []byte("Subject:")) {
		t.Fatalf("UID header fetch: %v", err)
	}
	if err := c.Logout().Wait(); err != nil {
		t.Fatal(err)
	}
}
func TestSMTPSpoolRoundTripAndRefresh(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.jsonl")
	cfg, _ := json.Marshal(map[string]any{"spool": path})
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	addr, _ := serve(t, s)
	c := client(t, addr)
	if err := c.Login("any", "dummy").Wait(); err != nil {
		t.Fatal(err)
	}
	selected, err := c.Select("INBOX", nil).Wait()
	if err != nil || selected.NumMessages != 0 {
		t.Fatal("initial mailbox not empty", err)
	}
	smtp, err := smtpserver.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	smtpAddr, _ := serve(t, smtp)
	raw := "From: a@example.test\r\nTo: b@example.test\r\nSubject: spooled\r\n\r\nfirst\r\n.dot\r\n"
	conn, err := net.DialTimeout("tcp", smtpAddr, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	sender := smtpclient.NewClient(conn)
	defer sender.Close()
	if err := sender.SendMail("a@example.test", []string{"b@example.test"}, strings.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	if err := c.Noop().Wait(); err != nil {
		t.Fatal(err)
	}
	section := &wire.FetchItemBodySection{Peek: true}
	for i := 0; i < 2; i++ {
		rows, err := c.Fetch(wire.SeqSetNum(1), &wire.FetchOptions{BodySection: []*wire.FetchItemBodySection{section}}).Collect()
		if err != nil || len(rows) != 1 {
			t.Fatalf("spool fetch: %v", err)
		}
		if string(rows[0].FindBodySection(section)) != raw {
			t.Fatal("spooled MIME bytes changed")
		}
	}
	status, err := c.Status("INBOX", &wire.StatusOptions{NumMessages: true}).Wait()
	if err != nil || *status.NumMessages != 1 {
		t.Fatal("spool duplicated on refresh", err)
	}
}
func TestCancelUnauthenticated(t *testing.T) {
	s, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	addr, cancel := serve(t, s)
	c := client(t, addr)
	if err := c.WaitGreeting(); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := c.Noop().Wait(); err == nil {
		t.Fatal("connection survived cancellation")
	}
}
func TestInvalidConfig(t *testing.T) {
	for _, raw := range []string{`[]`, `{"messages":[{"from":"not an address"}]}`, `{"messages":[{"from":"a@b","subject":"bad\r\ninjected"}]}`, `{"messages":[{"from":"a@b","date":"bad"}]}`} {
		if _, err := New(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted invalid config %s", raw)
		}
	}
}
