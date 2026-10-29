// Package amqp implements an in-memory AMQP 0-9-1 mock for lmclient's queue area.
// Config: {"users":{"u":"p"}}. Omitted/empty users accepts any PLAIN credentials;
// a nonempty map requires an exact pair (bad credentials receive ACCESS_REFUSED).
// Supports connection/channel handshakes, heartbeats, queue.declare, confirm.select,
// default-exchange basic.publish with complete-body confirms, and basic.get(noAck).
// FIFO queues persist across connections and are isolated by virtual-host name.
// Durable/persistent flags and message properties are accepted but not persisted;
// expiration properties do not expire messages. TLS, non-default exchanges,
// subscriptions, manual acknowledgments/requeue, transactions, exclusive/auto-delete
// queues and queue arguments are unsupported. Unknown routing keys get NOT_FOUND
// rather than RabbitMQ's default-exchange silent discard. No credentials are logged.
// Bounds: 128 connections, 128 channels/connection, 128 KiB frames, 8 MiB bodies,
// 1024 queues, 10000 queued messages and 32 MiB queued/in-flight content (per server).
// Idle connections time out after 60s when heartbeats are disabled; handshakes
// have 10s deadlines, writes 5s. Only loopback listeners are accepted.
package amqp

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/lmctl-ai/lmmock/internal/proto"
)

type config struct {
	Users map[string]string `json:"users"`
}
type message struct{ body, properties []byte }
type queue struct {
	durable  bool
	messages []message
}
type queueKey struct{ vhost, name string }
type server struct {
	cfg                       config
	mu                        sync.Mutex
	queues                    map[queueKey]*queue
	bytes, messages, inflight int
	nextQueue                 uint64
}

func init() { proto.Register("amqp", New) }
func New(raw json.RawMessage) (proto.Server, error) {
	var cfg config
	if len(raw) > 0 {
		d := json.NewDecoder(bytes.NewReader(raw))
		d.DisallowUnknownFields()
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || d.Decode(&cfg) != nil {
			return nil, proto.ConfigError("amqp expects an object with users (username/password string map)")
		}
		var more any
		if d.Decode(&more) != io.EOF {
			return nil, proto.ConfigError("amqp expects one config object")
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		if users, ok := fields["users"]; ok {
			var entries map[string]json.RawMessage
			if bytes.Equal(bytes.TrimSpace(users), []byte("null")) {
				return nil, proto.ConfigError("amqp users must be an object")
			}
			_ = json.Unmarshal(users, &entries)
			for _, p := range entries {
				if bytes.Equal(bytes.TrimSpace(p), []byte("null")) {
					return nil, proto.ConfigError("amqp passwords must be strings")
				}
			}
		}
	}
	return &server{cfg: cfg, queues: map[queueKey]*queue{}}, nil
}
func (*server) Scheme() string { return "amqp" }
func (s *server) Serve(ctx context.Context, ln net.Listener) error {
	defer ln.Close()
	a, ok := ln.Addr().(*net.TCPAddr)
	if !ok || !a.IP.IsLoopback() {
		return errors.New("amqp requires a loopback TCP listener")
	}
	var mu sync.Mutex
	active := map[net.Conn]bool{}
	var wg sync.WaitGroup
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			ln.Close()
		case <-done:
		}
	}()
	defer func() {
		mu.Lock()
		for c := range active {
			c.Close()
		}
		mu.Unlock()
		wg.Wait()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		mu.Lock()
		if len(active) >= 128 {
			mu.Unlock()
			c.Close()
			continue
		}
		active[c] = true
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { c.Close(); mu.Lock(); delete(active, c); mu.Unlock() }()
			p := &peer{server: s, conn: c, frameMax: frameLimit, channelMax: 128, channels: map[uint16]*channel{}}
			p.run()
		}()
	}
}

type pending struct {
	key              queueKey
	body, properties []byte
	size             uint64
	header           bool
	reserved         int
}
type channel struct {
	confirm, closing     bool
	published, delivered uint64
	pending              *pending
	lastQueue            string
}
type peer struct {
	server     *server
	conn       net.Conn
	writeMu    sync.Mutex
	frameMax   uint32
	channelMax uint16
	heartbeat  uint16
	channels   map[uint16]*channel
	vhost      string
}
type fault struct {
	code          uint16
	text          string
	class, method uint16
}

func fail(code uint16, text string) *fault { return &fault{code: code, text: text} }
func (p *peer) send(f frame) error {
	if len(f.payload)+8 > int(p.frameMax) {
		return errWire
	}
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return writeFrame(p.conn, f)
}
func (p *peer) method(ch, class, method uint16, args encoder) error {
	var b encoder
	b.u16(class)
	b.u16(method)
	b = append(b, args...)
	return p.send(frame{1, ch, b})
}
func (p *peer) next(timeout time.Duration) (frame, error) {
	p.conn.SetReadDeadline(time.Now().Add(timeout))
	return readFrame(p.conn, p.frameMax)
}
func (p *peer) expect(class, method uint16) (decoder, error) {
	for {
		f, err := p.next(10 * time.Second)
		if err != nil {
			return decoder{}, err
		}
		if f.kind == 8 {
			continue
		}
		d := decoder{p: f.payload}
		if f.kind != 1 || f.channel != 0 || d.u16() != class || d.u16() != method {
			return decoder{}, errWire
		}
		return d, nil
	}
}
func (p *peer) closeError(ch uint16, e *fault) {
	var b encoder
	b.u16(e.code)
	b.short(e.text)
	b.u16(e.class)
	b.u16(e.method)
	if ch == 0 {
		_ = p.method(0, 10, 50, b)
		p.conn.SetReadDeadline(time.Now().Add(time.Second))
		_, _ = readFrame(p.conn, p.frameMax)
	} else {
		_ = p.method(ch, 20, 40, b)
		if c := p.channels[ch]; c != nil {
			c.closing = true
			p.drop(c)
		}
	}
}
func (p *peer) handshake() bool {
	var header [8]byte
	p.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(p.conn, header[:]); err != nil {
		return false
	}
	if string(header[:]) != "AMQP\x00\x00\x09\x01" {
		_, _ = p.conn.Write([]byte("AMQP\x00\x00\x09\x01"))
		return false
	}
	var start encoder
	start.u8(0)
	start.u8(9)
	start.long(nil)
	start.long([]byte("PLAIN"))
	start.long([]byte("en_US"))
	if p.method(0, 10, 10, start) != nil {
		return false
	}
	d, err := p.expect(10, 11)
	if err != nil {
		return false
	}
	d.long()
	mechanism := d.short()
	response := d.long()
	locale := d.short()
	parts := bytes.Split(response, []byte{0})
	valid := d.done() && mechanism == "PLAIN" && locale == "en_US" && len(parts) == 3
	if valid && len(p.server.cfg.Users) > 0 {
		password, ok := p.server.cfg.Users[string(parts[1])]
		valid = ok && subtle.ConstantTimeCompare([]byte(password), parts[2]) == 1
	}
	if !valid {
		p.closeError(0, &fault{403, "ACCESS_REFUSED - authentication failed", 10, 11})
		return false
	}
	var tune encoder
	tune.u16(128)
	tune.u32(frameLimit)
	tune.u16(0)
	if p.method(0, 10, 30, tune) != nil {
		return false
	}
	d, err = p.expect(10, 31)
	if err != nil {
		return false
	}
	cm, fm, hb := d.u16(), d.u32(), d.u16()
	if !d.done() || cm > 128 || (fm != 0 && (fm < 4096 || fm > frameLimit)) {
		p.closeError(0, fail(501, "FRAME_ERROR - invalid tune"))
		return false
	}
	if cm != 0 {
		p.channelMax = cm
	}
	if fm != 0 {
		p.frameMax = fm
	}
	p.heartbeat = hb
	d, err = p.expect(10, 40)
	if err != nil {
		return false
	}
	p.vhost = d.short()
	d.short()
	d.u8()
	if !d.done() {
		return false
	}
	var open encoder
	open.short("")
	return p.method(0, 10, 41, open) == nil
}
func (p *peer) run() {
	defer func() {
		for _, c := range p.channels {
			p.drop(c)
		}
	}()
	if !p.handshake() {
		return
	}
	stop := make(chan struct{})
	var hb sync.WaitGroup
	defer func() { close(stop); hb.Wait() }()
	idle := 60 * time.Second
	if p.heartbeat > 0 {
		idle = time.Duration(p.heartbeat) * 2 * time.Second
		hb.Add(1)
		go func() {
			defer hb.Done()
			ticker := time.NewTicker(time.Duration(p.heartbeat) * time.Second / 2)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return
				case <-ticker.C:
					if p.send(frame{kind: 8}) != nil {
						p.conn.Close()
						return
					}
				}
			}
		}()
	}
	for {
		f, err := p.next(idle)
		if err != nil {
			if errors.Is(err, errWire) {
				p.closeError(0, fail(501, "FRAME_ERROR"))
			}
			return
		}
		if f.kind == 8 {
			continue
		}
		if f.kind == 1 {
			d := decoder{p: f.payload}
			class, method := d.u16(), d.u16()
			if d.bad {
				p.closeError(0, fail(501, "FRAME_ERROR"))
				return
			}
			if class == 10 && method == 50 && f.channel == 0 {
				d.u16()
				d.short()
				d.u16()
				d.u16()
				if d.done() {
					_ = p.method(0, 10, 51, nil)
				}
				return
			}
			if f.channel == 0 || f.channel > p.channelMax {
				p.closeError(0, fail(504, "CHANNEL_ERROR"))
				return
			}
			if class == 20 && method == 10 {
				d.short()
				if !d.done() || p.channels[f.channel] != nil {
					p.closeError(0, fail(504, "CHANNEL_ERROR"))
					return
				}
				p.channels[f.channel] = &channel{}
				var b encoder
				b.long(nil)
				if p.method(f.channel, 20, 11, b) != nil {
					return
				}
				continue
			}
			c := p.channels[f.channel]
			if c == nil {
				p.closeError(0, fail(504, "CHANNEL_ERROR"))
				return
			}
			if class == 20 && method == 41 && c.closing {
				delete(p.channels, f.channel)
				continue
			}
			if c.closing {
				continue
			}
			if class == 20 && method == 40 {
				d.u16()
				d.short()
				d.u16()
				d.u16()
				if !d.done() {
					return
				}
				if p.method(f.channel, 20, 41, nil) != nil {
					return
				}
				p.drop(c)
				delete(p.channels, f.channel)
				continue
			}
			if c.pending != nil {
				p.closeError(0, fail(505, "UNEXPECTED_FRAME"))
				return
			}
			e := p.dispatch(f.channel, c, class, method, &d)
			if e != nil {
				e.class = class
				e.method = method
				p.closeError(f.channel, e)
			}
		} else {
			c := p.channels[f.channel]
			if c == nil || c.closing || c.pending == nil {
				p.closeError(0, fail(505, "UNEXPECTED_FRAME"))
				return
			}
			if e := p.content(f, c); e != nil {
				p.closeError(f.channel, e)
			}
		}
	}
}
