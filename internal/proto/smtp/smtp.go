// Package smtp provides a loopback SMTP sink with AUTH PLAIN and LOGIN.
// Config: {"users":{"test":"secret"},"spool":"/path/mail.jsonl"}.
// Absent users accepts any credentials; configured users reject bad AUTH (535).
// Unauthenticated delivery is allowed, matching the lmclient mail fixtures.
// Messages are retained in memory and optionally appended to the shared JSONL
// spool (mailstore.Message, raw RFC822 data encoded as base64). One SMTP writer
// per spool; no relay, TLS, or recipient filtering. Message limit: 10 MiB;
// in-memory messages and on-disk spool each have a 64 MiB limit.
package smtp

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-sasl"
	wire "github.com/emersion/go-smtp"
	"github.com/lmctl-ai/lmmock/internal/proto"
	"github.com/lmctl-ai/lmmock/internal/proto/mailstore"
)

func init() { proto.Register("smtp", New) }

type Server struct {
	cfg      mailstore.Config
	mu       sync.Mutex
	messages []mailstore.Message
	bytes    int
}

func New(raw json.RawMessage) (proto.Server, error) {
	cfg, seeds, err := mailstore.Parse(raw)
	if err != nil {
		return nil, err
	}
	size := 0
	for _, m := range seeds {
		size += len(m.Data)
	}
	return &Server{cfg: cfg, messages: seeds, bytes: size}, nil
}
func (*Server) Scheme() string { return "smtp" }
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	ln = mailstore.TrackListener(ln)
	server := wire.NewServer(wire.BackendFunc(func(*wire.Conn) (wire.Session, error) { return &session{server: s}, nil }))
	server.Domain = "localhost"
	server.AllowInsecureAuth = true
	server.MaxMessageBytes = mailstore.MaxMessage
	server.MaxRecipients = 1000
	server.ReadTimeout = 30 * time.Second
	server.WriteTimeout = 30 * time.Second
	server.ErrorLog = log.New(io.Discard, "", 0)
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

// Messages returns a snapshot, without sharing mutable message byte slices.
func (s *Server) Messages() []mailstore.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]mailstore.Message, len(s.messages))
	for i, m := range s.messages {
		m.Data = append([]byte(nil), m.Data...)
		m.To = append([]string(nil), m.To...)
		out[i] = m
	}
	return out
}

type session struct {
	server *Server
	from   string
	to     []string
}

func (s *session) Reset()                                      { s.from = ""; s.to = nil }
func (s *session) Logout() error                               { return nil }
func (s *session) Mail(from string, _ *wire.MailOptions) error { s.Reset(); s.from = from; return nil }
func (s *session) Rcpt(to string, _ *wire.RcptOptions) error   { s.to = append(s.to, to); return nil }
func (s *session) Data(r io.Reader) error {
	data, err := io.ReadAll(io.LimitReader(r, mailstore.MaxMessage+1))
	if err != nil {
		return err
	}
	if len(data) > mailstore.MaxMessage {
		return &wire.SMTPError{Code: 552, Message: "Message too large"}
	}
	m := mailstore.Message{From: s.from, To: append([]string(nil), s.to...), Data: data, Date: mailstore.Epoch}
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	if s.server.bytes+len(data) > mailstore.MaxSpool {
		return &wire.SMTPError{Code: 452, Message: "Message store full"}
	}
	if s.server.cfg.Spool != "" {
		if err := mailstore.Append(s.server.cfg.Spool, m); err != nil {
			return &wire.SMTPError{Code: 451, Message: "Cannot store message"}
		}
	}
	s.server.messages = append(s.server.messages, m)
	s.server.bytes += len(data)
	return nil
}
func (s *session) AuthMechanisms() []string { return []string{sasl.Plain, sasl.Login} }
func (s *session) Auth(mech string) (sasl.Server, error) {
	check := func(user, password string) error {
		if !s.server.cfg.Accept(user, password) {
			return wire.ErrAuthFailed
		}
		return nil
	}
	switch mech {
	case sasl.Plain:
		return sasl.NewPlainServer(func(identity, user, password string) error {
			if identity != "" && identity != user {
				return wire.ErrAuthFailed
			}
			return check(user, password)
		}), nil
	case sasl.Login:
		return &login{check: check}, nil
	default:
		return nil, wire.ErrAuthUnknownMechanism
	}
}

type login struct {
	step  int
	user  string
	check func(string, string) error
}

func (l *login) Next(response []byte) ([]byte, bool, error) {
	if l.step == 0 {
		l.step = 1
		if len(response) == 0 {
			return []byte("Username:"), false, nil
		}
	}
	if l.step == 1 {
		l.user = string(response)
		l.step = 2
		return []byte("Password:"), false, nil
	}
	if l.step == 2 {
		l.step = 3
		return nil, true, l.check(l.user, string(response))
	}
	return nil, true, wire.ErrAuthFailed
}
