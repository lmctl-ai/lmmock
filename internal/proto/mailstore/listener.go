package mailstore

import (
	"net"
	"sync"
	"time"
)

// TrackListener closes every accepted connection, including those which the
// protocol library has not yet registered in its own per-connection goroutine.
func TrackListener(ln net.Listener) net.Listener {
	return &listener{Listener: ln, conns: make(map[*connection]struct{})}
}

type listener struct {
	net.Listener
	mu     sync.Mutex
	closed bool
	conns  map[*connection]struct{}
}

func (l *listener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	c := &connection{Conn: conn, owner: l}
	l.conns[c] = struct{}{}
	return c, nil
}
func (l *listener) Close() error {
	err := l.Listener.Close()
	l.mu.Lock()
	l.closed = true
	var conns []*connection
	for c := range l.conns {
		conns = append(conns, c)
	}
	l.mu.Unlock()
	for _, c := range conns {
		_ = c.Close()
	}
	return err
}

type connection struct {
	net.Conn
	owner *listener
}

func (c *connection) Close() error {
	err := c.Conn.Close()
	c.owner.mu.Lock()
	delete(c.owner.conns, c)
	c.owner.mu.Unlock()
	return err
}
func bounded(t time.Time) time.Time {
	limit := time.Now().Add(time.Minute)
	if t.IsZero() || t.After(limit) {
		return limit
	}
	return t
}
func (c *connection) SetReadDeadline(t time.Time) error  { return c.Conn.SetReadDeadline(bounded(t)) }
func (c *connection) SetWriteDeadline(t time.Time) error { return c.Conn.SetWriteDeadline(bounded(t)) }
func (c *connection) SetDeadline(t time.Time) error      { return c.Conn.SetDeadline(bounded(t)) }
