// Package mailstore shares the SMTP/IMAP fixture config and append-only spool format.
// A spool line is {"from":string,"to":[string],"data":base64-RFC822,"date":RFC3339}.
// One SMTP process owns a spool; any number of IMAP processes may read it.
package mailstore

import (
	"bufio"
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
)

const MaxMessage = 10 * 1024 * 1024
const MaxSpool = 64 * 1024 * 1024

var Epoch = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

type Config struct {
	Users    map[string]string `json:"users"`
	Spool    string            `json:"spool"`
	Messages []Seed            `json:"messages"`
}
type Seed struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
	Date    string   `json:"date"`
}
type Message struct {
	From string    `json:"from"`
	To   []string  `json:"to"`
	Data []byte    `json:"data"`
	Date time.Time `json:"date"`
}

func Parse(raw json.RawMessage) (Config, []Message, error) {
	var cfg Config
	if len(raw) > 0 {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if err := d.Decode(&cfg); err != nil {
			return cfg, nil, proto.ConfigError("invalid mail configuration")
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return cfg, nil, proto.ConfigError("expected one config object")
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return cfg, nil, proto.ConfigError("expected config object")
		}
	}
	var messages []Message
	total := 0
	for _, seed := range cfg.Messages {
		m, err := seed.Message()
		if err != nil {
			return cfg, nil, proto.ConfigError("invalid seeded mail message")
		}
		total += len(m.Data)
		if total > MaxSpool {
			return cfg, nil, proto.ConfigError("seed messages exceed 64 MiB")
		}
		messages = append(messages, m)
	}
	return cfg, messages, nil
}
func (c Config) Accept(user, password string) bool {
	if c.Users == nil {
		return true
	}
	expected, ok := c.Users[user]
	return ok && subtle.ConstantTimeCompare([]byte(expected), []byte(password)) == 1
}
func (s Seed) Message() (Message, error) {
	date := Epoch
	if s.Date != "" {
		var err error
		date, err = time.Parse(time.RFC3339, s.Date)
		if err != nil {
			return Message{}, err
		}
	}
	for _, h := range append([]string{s.From, s.Subject}, s.To...) {
		if strings.ContainsAny(h, "\r\n") {
			return Message{}, errors.New("invalid header")
		}
	}
	from, err := mail.ParseAddress(s.From)
	if err != nil {
		return Message{}, err
	}
	var to []string
	for _, a := range s.To {
		addr, err := mail.ParseAddress(a)
		if err != nil {
			return Message{}, err
		}
		to = append(to, addr.String())
	}
	body := strings.ReplaceAll(strings.ReplaceAll(s.Body, "\r\n", "\n"), "\n", "\r\n")
	data := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n%s", from.String(), strings.Join(to, ", "), mime.QEncoding.Encode("utf-8", s.Subject), date.Format(time.RFC1123Z), body))
	if len(data) > MaxMessage {
		return Message{}, errors.New("message too large")
	}
	return Message{From: s.From, To: s.To, Data: data, Date: date}, nil
}

// Append writes a whole line under the caller's lock. Readers ignore a final
// incomplete line; failed writes are rolled back so later appends remain valid.
func Append(path string, m Message) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if info.Size()+int64(len(data)) > MaxSpool {
		return errors.New("spool full")
	}
	n, err := f.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		_ = f.Truncate(info.Size())
		return err
	}
	return f.Close()
}

// Read returns complete records only. Missing spools are empty mailboxes.
func Read(path string) ([]Message, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxSpool {
		return nil, errors.New("spool too large")
	}
	r := bufio.NewReader(io.LimitReader(f, MaxSpool+1))
	var messages []Message
	for {
		line, err := r.ReadBytes('\n')
		if err == io.EOF {
			return messages, nil
		}
		if err != nil {
			return nil, err
		}
		var m Message
		if json.Unmarshal(line, &m) != nil || len(m.Data) > MaxMessage || m.Date.IsZero() {
			return nil, errors.New("invalid spool record")
		}
		messages = append(messages, m)
	}
}
