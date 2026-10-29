// Package imap provides an in-memory IMAP4rev1 mailbox using go-imap's backend.
// Config: {"users":{"test":"secret"},"spool":"/path/mail.jsonl",
// "messages":[{"from":"a@example.test","to":["b@example.test"],
// "subject":"hello","body":"text","date":"2020-01-01T00:00:00Z"}]}.
// All users share INBOX. Omitted users accepts any login; {} rejects all logins.
// Seed dates default to 2020-01-01 UTC. Seeds precede spool messages. The spool
// is append-only, single-SMTP-writer JSONL; reads refresh at login and commands,
// ignore incomplete tail records, and preserve raw MIME bytes. FETCH/UID FETCH,
// SEARCH/UID SEARCH, LIST, SELECT/EXAMINE and STATUS use the library backend.
// No TLS, delivery routing, or persistent IMAP mutations/flags are provided.
package imap

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"strings"
	"sync"

	wire "github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"github.com/lmctl-ai/lmmock/internal/proto"
	"github.com/lmctl-ai/lmmock/internal/proto/mailstore"
)

func init() { proto.Register("imap", New) }

type Server struct {
	cfg    mailstore.Config
	user   *imapmemserver.User
	mu     sync.Mutex
	loaded int
}

func New(raw json.RawMessage) (proto.Server, error) {
	cfg, seeds, err := mailstore.Parse(raw)
	if err != nil {
		return nil, err
	}
	s := &Server{cfg: cfg, user: imapmemserver.NewUser("fixture", "")}
	if err = s.user.Create("INBOX", nil); err != nil {
		return nil, err
	}
	for _, m := range seeds {
		if err = s.append(m); err != nil {
			return nil, proto.ConfigError("invalid seeded message")
		}
	}
	return s, nil
}
func (*Server) Scheme() string { return "imap" }
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ln = mailstore.TrackListener(ln)
	server := imapserver.New(&imapserver.Options{InsecureAuth: true, Logger: log.New(io.Discard, "", 0), NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return &session{server: s}, nil, nil
	}})
	var once sync.Once
	shutdown := func() { once.Do(func() { _ = ln.Close(); _ = server.Close() }) }
	stop := context.AfterFunc(ctx, shutdown)
	defer stop()
	defer shutdown()
	err := server.Serve(ln)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

type literal struct{ *bytes.Reader }

func (l literal) Size() int64 { return l.Reader.Size() }
func (s *Server) append(m mailstore.Message) error {
	_, err := s.user.Append("INBOX", literal{bytes.NewReader(m.Data)}, &wire.AppendOptions{Time: m.Date})
	return err
}
func (s *Server) refresh() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	messages, err := mailstore.Read(s.cfg.Spool)
	if err != nil || len(messages) < s.loaded {
		return &wire.Error{Type: wire.StatusResponseTypeNo, Text: "Cannot read append-only mail spool"}
	}
	for _, m := range messages[s.loaded:] {
		if err = s.append(m); err != nil {
			return &wire.Error{Type: wire.StatusResponseTypeNo, Text: "Cannot load mail spool"}
		}
		s.loaded++
	}
	return nil
}

type session struct {
	*imapmemserver.UserSession
	server *Server
}

func (s *session) Login(user, password string) error {
	if !s.server.cfg.Accept(user, password) {
		return imapserver.ErrAuthFailed
	}
	if err := s.server.refresh(); err != nil {
		return err
	}
	s.UserSession = imapmemserver.NewUserSession(s.server.user)
	return nil
}
func (s *session) Close() error { return s.UserSession.Close() }
func inbox(name string) string {
	if strings.EqualFold(name, "INBOX") {
		return "INBOX"
	}
	return name
}
func (s *session) Select(name string, o *wire.SelectOptions) (*wire.SelectData, error) {
	if err := s.server.refresh(); err != nil {
		return nil, err
	}
	return s.UserSession.Select(inbox(name), o)
}
func (s *session) List(w *imapserver.ListWriter, ref string, patterns []string, o *wire.ListOptions) error {
	if err := s.server.refresh(); err != nil {
		return err
	}
	return s.UserSession.List(w, ref, patterns, o)
}
func (s *session) Status(name string, o *wire.StatusOptions) (*wire.StatusData, error) {
	if err := s.server.refresh(); err != nil {
		return nil, err
	}
	return s.UserSession.Status(inbox(name), o)
}
func (s *session) Poll(w *imapserver.UpdateWriter, expunge bool) error {
	if s.UserSession == nil {
		return nil
	}
	if err := s.server.refresh(); err != nil {
		return err
	}
	return s.UserSession.Poll(w, expunge)
}
func (s *session) Fetch(w *imapserver.FetchWriter, set wire.NumSet, o *wire.FetchOptions) error {
	if err := s.server.refresh(); err != nil {
		return err
	}
	return s.UserSession.Fetch(w, set, o)
}
func (s *session) Search(kind imapserver.NumKind, c *wire.SearchCriteria, o *wire.SearchOptions) (*wire.SearchData, error) {
	if err := s.server.refresh(); err != nil {
		return nil, err
	}
	return s.UserSession.Search(kind, c, o)
}
