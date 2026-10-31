// Package h2 uses the Go HTTP/2 transport and flushes each lmcode send as an event.
package h2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"golang.org/x/net/http/httpguts"
	"io"
	"net"
	"net/http"
	"strings"
)

func init() {
	wire.Register("http2", wire.Definition{SAM: "HTTP2Handler", New: func(_ string, raw json.RawMessage) (wire.Layer, error) {
		var options struct{}
		if err := wire.Decode(raw, &options); err != nil {
			return nil, err
		}
		return Layer{}, nil
	}})
}

type Layer struct{}

func (Layer) Scheme() string { return "http" }
func (Layer) ValidateRoute(key string) error {
	if key == "*" {
		return nil
	}
	parts := strings.SplitN(key, " ", 2)
	if len(parts) != 2 || !strings.Contains(" GET POST PUT PATCH DELETE HEAD OPTIONS ", " "+parts[0]+" ") || !strings.HasPrefix(parts[1], "/") || strings.ContainsAny(parts[1], "?#\r\n ") {
		return fmt.Errorf("invalid HTTP/2 binding %q", key)
	}
	return nil
}
func (Layer) Serve(ctx context.Context, ln net.Listener, e *wire.Engine) error {
	return wire.ServeHTTP(ctx, ln, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		if !e.Has(key) {
			http.NotFound(w, r)
			return
		}
		s := &session{request: r, writer: w}
		if err := e.Dispatch(key, s); err != nil {
			if s.sent {
				panic(http.ErrAbortHandler)
			}
			code := 500
			var failure *statusError
			if errors.As(err, &failure) {
				code = failure.code
			}
			http.Error(w, err.Error(), code)
		} else if !s.sent {
			w.WriteHeader(s.statusCode())
		}
	}), wire.HTTP2Protocols())
}

type statusError struct {
	code    int
	message string
}

func (e *statusError) Error() string { return e.message }

type session struct {
	request        *http.Request
	writer         http.ResponseWriter
	received, sent bool
	status         int
}

func (s *session) Receive() (string, string, error) {
	if s.received {
		return "", "", io.EOF
	}
	s.received = true
	b, err := io.ReadAll(s.request.Body)
	return "body", string(b), err
}
func (s *session) Send(kind, body string) error {
	if kind != "" {
		return errors.New("HTTP/2 uses byte DATA events, not WebSocket binary frames")
	}
	if !s.sent {
		s.writer.WriteHeader(s.statusCode())
		s.sent = true
	}
	if _, err := io.WriteString(s.writer, body); err != nil {
		return err
	}
	return http.NewResponseController(s.writer).Flush()
}
func (s *session) Header(name string) string { return wire.HTTPHeader(s.request, name) }
func (s *session) SetHeader(name, value string) error {
	if !httpguts.ValidHeaderFieldName(name) || !httpguts.ValidHeaderFieldValue(value) {
		return errors.New("invalid HTTP/2 response header")
	}
	if s.sent {
		return errors.New("response headers already sent")
	}
	s.writer.Header().Set(name, value)
	return nil
}
func (s *session) Fail(code int, message string) error {
	if code < 400 || code > 599 {
		return errors.New("HTTP/2 failure status must be 400..599")
	}
	return &statusError{code, message}
}

func (s *session) statusCode() int {
	if s.status == 0 {
		return http.StatusOK
	}
	return s.status
}
func (s *session) SetStatus(code int) error {
	if s.sent {
		return errors.New("response headers already sent")
	}
	if code < 200 || code > 599 {
		return errors.New("HTTP/2 final status must be 200..599")
	}
	s.status = code
	return nil
}
