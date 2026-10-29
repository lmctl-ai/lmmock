// Package mqtt wraps the MIT-licensed Mochi in-memory MQTT broker.
// Config: {"users":{"u":"p"}}. Omit users (or use {}) for allow-all auth;
// a nonempty users map requires an exact username/password pair. Authenticated
// clients may read/write all topics. Unknown fields/types are rejected.
// MQTT 3.1.1 QoS 0/1/2, wildcard subscriptions and retained messages are supported
// by the broker (also MQTT 5). There is no TLS, disk persistence or external ACL.
// Only loopback TCP listeners are accepted. Limits: 128 connected clients,
// 1 MiB packets, 60-second idle reads, 5-second writes. No broker logs are emitted.
package mqtt

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
	broker "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

type config struct {
	Users map[string]string `json:"users"`
}
type server struct{ cfg config }

func init() { proto.Register("mqtt", New) }
func New(raw json.RawMessage) (proto.Server, error) {
	var cfg config
	if len(raw) > 0 {
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.DisallowUnknownFields()
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || dec.Decode(&cfg) != nil {
			return nil, proto.ConfigError("mqtt expects an object with users (username/password string map)")
		}
		// encoding/json accepts null for scalar/map fields; the config schema does not.
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		for _, field := range fields {
			if bytes.Equal(bytes.TrimSpace(field), []byte("null")) {
				return nil, proto.ConfigError("mqtt config fields must not be null")
			}
		}
		if users, ok := fields["users"]; ok {
			var entries map[string]json.RawMessage
			_ = json.Unmarshal(users, &entries)
			for _, password := range entries {
				if bytes.Equal(bytes.TrimSpace(password), []byte("null")) {
					return nil, proto.ConfigError("mqtt passwords must be strings")
				}
			}
		}
		var extra any
		if dec.Decode(&extra) != io.EOF {
			return nil, proto.ConfigError("mqtt expects one configuration object")
		}
	}
	return &server{cfg: cfg}, nil
}
func (*server) Scheme() string { return "mqtt" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !addr.IP.IsLoopback() {
		return errors.New("mqtt requires a loopback TCP listener")
	}
	caps := broker.NewDefaultServerCapabilities()
	caps.MaximumPacketSize = 1024 * 1024
	caps.MaximumClients = 128
	b := broker.New(&broker.Options{Capabilities: caps, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := b.AddHook(&authHook{users: s.cfg.Users}, nil); err != nil {
		return err
	}
	if err := b.Serve(); err != nil {
		return err
	}
	defer b.Close()
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	var workers sync.WaitGroup
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			ln.Close()
		case <-done:
		}
	}()
	// Own the accept loop so pre-CONNECT sockets are also closed on cancellation.
	defer func() {
		mu.Lock()
		for c := range active {
			c.Close()
		}
		mu.Unlock()
		workers.Wait()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		c = &boundedConn{Conn: c}
		mu.Lock()
		active[c] = true
		mu.Unlock()
		workers.Add(1)
		go func(c net.Conn) {
			defer workers.Done()
			defer func() { c.Close(); mu.Lock(); delete(active, c); mu.Unlock() }()
			_ = b.EstablishConnection("mock", c)
		}(c)
	}
}

type boundedConn struct{ net.Conn }

func (c *boundedConn) Read(p []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	return c.Conn.Read(p)
}
func (c *boundedConn) Write(p []byte) (int, error) {
	c.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.Conn.Write(p)
}

type authHook struct {
	broker.HookBase
	users map[string]string
}

func (*authHook) ID() string { return "lmmock-auth" }
func (*authHook) Provides(b byte) bool {
	return b == broker.OnConnectAuthenticate || b == broker.OnACLCheck
}
func (h *authHook) OnConnectAuthenticate(_ *broker.Client, pk packets.Packet) bool {
	if len(h.users) == 0 {
		return true
	}
	password, ok := h.users[string(pk.Connect.Username)]
	return ok && subtle.ConstantTimeCompare(pk.Connect.Password, []byte(password)) == 1
}
func (*authHook) OnACLCheck(_ *broker.Client, _ string, _ bool) bool { return true }
