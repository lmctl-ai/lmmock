// Package kafka serves a single plaintext, in-memory Kafka broker using kfake.
// Config is optional: {"topics":{"events":1,"other":2}} seeds topic partition
// counts (1..100, at most 100 topics and 1000 total partitions). Unknown fields
// are rejected. Missing topics auto-create with one partition. No persistence,
// authentication, TLS, replication, or production-broker fidelity is promised.
// kfake uses the supplied listener directly, including its advertised address.
// Connections have a renewable one-minute I/O deadline and close on cancellation.
package kafka

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
	"github.com/twmb/franz-go/pkg/kfake"
)

func init() { proto.Register("kafka", New) }

type server struct {
	Topics map[string]int32 `json:"topics"`
}

var topicName = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,249}$`)

// New validates configuration without opening any sockets.
func New(cfg json.RawMessage) (proto.Server, error) {
	s := new(server)
	if len(cfg) > 0 {
		if bytes.Equal(bytes.TrimSpace(cfg), []byte("null")) {
			return nil, proto.ConfigError("expected an object")
		}
		d := json.NewDecoder(bytes.NewReader(cfg))
		d.DisallowUnknownFields()
		if err := d.Decode(s); err != nil {
			return nil, proto.ConfigError("%v", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return nil, proto.ConfigError("expected one object")
		}
	}
	total := int32(0)
	for topic, n := range s.Topics {
		if !topicName.MatchString(topic) || topic == "." || topic == ".." || n < 1 || n > 100 {
			return nil, proto.ConfigError("invalid topic name or partition count")
		}
		total += n
	}
	if len(s.Topics) > 100 || total > 1000 {
		return nil, proto.ConfigError("too many topics or partitions")
	}
	return s, nil
}
func (*server) Scheme() string { return "kafka" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	host, _, err := net.SplitHostPort(ln.Addr().String())
	if err != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("kafka requires a loopback listener")
	}
	if ctx.Err() != nil {
		return nil
	}
	l := &listener{Listener: ln, conns: make(map[*connection]struct{}), failed: make(chan error, 1)}
	defer l.Close()
	opts := []kfake.Opt{kfake.NumBrokers(1), kfake.DefaultNumPartitions(1), kfake.AllowAutoTopicCreation(), kfake.ListenFn(func(string, string) (net.Listener, error) { return l, nil })}
	for topic, n := range s.Topics {
		opts = append(opts, kfake.SeedTopics(n, topic))
	}
	cluster, err := kfake.NewCluster(opts...)
	if err != nil {
		return err
	}
	defer cluster.Close()
	select {
	case <-ctx.Done():
		return nil
	case err := <-l.failed:
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
}

// Track accepted sockets independently of kfake so Serve closes even idle or
// partial-frame connections before returning, on every supported platform.
type listener struct {
	net.Listener
	mu     sync.Mutex
	closed bool
	conns  map[*connection]struct{}
	failed chan error
}

func (l *listener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		select {
		case l.failed <- err:
		default:
		}
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		c.Close()
		return nil, net.ErrClosed
	}
	wrapped := &connection{Conn: c, owner: l}
	l.conns[wrapped] = struct{}{}
	return wrapped, nil
}
func (l *listener) Close() error {
	err := l.Listener.Close()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.closed = true
	for c := range l.conns {
		c.Conn.Close()
		delete(l.conns, c)
	}
	return err
}

type connection struct {
	net.Conn
	owner *listener
}

func (c *connection) Read(b []byte) (int, error) {
	c.SetReadDeadline(time.Now().Add(time.Minute))
	return c.Conn.Read(b)
}
func (c *connection) Write(b []byte) (int, error) {
	c.SetWriteDeadline(time.Now().Add(time.Minute))
	return c.Conn.Write(b)
}
func (c *connection) Close() error {
	err := c.Conn.Close()
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return err
}
