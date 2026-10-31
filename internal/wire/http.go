package wire

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
)

type httpOwnerKey struct{}
type httpOwner struct {
	mu      sync.Mutex
	stopped bool
	conns   map[net.Conn]bool
	work    sync.WaitGroup
}
type httpConnection struct {
	owner *httpOwner
	conn  net.Conn
}

// ReleaseHijacked transfers connection cleanup back to the protocol handler.
// Call after its final Close; ordinary HTTP connections use ConnState instead.
func ReleaseHijacked(ctx context.Context) {
	if c, ok := ctx.Value(httpOwnerKey{}).(httpConnection); ok {
		c.owner.mu.Lock()
		delete(c.owner.conns, c.conn)
		c.owner.mu.Unlock()
	}
}
func HTTP2Protocols() *http.Protocols {
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	p.SetHTTP2(true)
	return p
}

// ServeHTTP closes accepted and hijacked connections on cancellation and waits
// for entered handlers. No listener deadline or application event timer is used.
func ServeHTTP(ctx context.Context, ln net.Listener, handler http.Handler, protocols *http.Protocols) error {
	owner := &httpOwner{conns: map[net.Conn]bool{}}
	srv := &http.Server{Protocols: protocols}
	srv.ConnContext = func(ctx context.Context, c net.Conn) context.Context {
		return context.WithValue(ctx, httpOwnerKey{}, httpConnection{owner, c})
	}
	srv.ConnState = func(c net.Conn, s http.ConnState) {
		owner.mu.Lock()
		defer owner.mu.Unlock()
		if s == http.StateNew {
			if owner.stopped {
				_ = c.Close()
			} else {
				owner.conns[c] = true
			}
		}
		if s == http.StateClosed {
			delete(owner.conns, c)
		}
	}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		owner.mu.Lock()
		if owner.stopped {
			owner.mu.Unlock()
			return
		}
		owner.work.Add(1)
		owner.mu.Unlock()
		defer owner.work.Done()
		handler.ServeHTTP(w, r)
	})
	var once sync.Once
	closeAll := func() {
		once.Do(func() {
			owner.mu.Lock()
			owner.stopped = true
			conns := make([]net.Conn, 0, len(owner.conns))
			for c := range owner.conns {
				conns = append(conns, c)
			}
			owner.mu.Unlock()
			_ = ln.Close()
			_ = srv.Close()
			for _, c := range conns {
				_ = c.Close()
			}
			owner.work.Wait()
		})
	}
	stop := context.AfterFunc(ctx, closeAll)
	defer stop()
	defer closeAll()
	if ctx.Err() != nil {
		return nil
	}
	err := srv.Serve(ln)
	if ctx.Err() != nil || errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// HTTPHeader exposes parsed pseudo-headers, including the original query URI.
func HTTPHeader(r *http.Request, name string) string {
	switch strings.ToLower(name) {
	case ":authority", "host":
		return r.Host
	case ":path":
		return r.URL.RequestURI()
	case ":method":
		return r.Method
	case ":scheme":
		if r.TLS != nil {
			return "https"
		}
		return "http"
	}
	return r.Header.Get(name)
}
