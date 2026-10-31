// Package ws provides RFC 6455 framing; message policy belongs to WebSocketHandler.
package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"golang.org/x/net/http/httpguts"
	"io"
	"net"
	"net/http"
	"strings"
	"unicode/utf8"
)

func init() {
	wire.Register("ws", wire.Definition{SAM: "WebSocketHandler", New: func(_ string, raw json.RawMessage) (wire.Layer, error) {
		var options Options
		if err := wire.Decode(raw, &options); err != nil {
			return nil, err
		}
		for _, p := range options.Subprotocols {
			if !httpguts.ValidHeaderFieldName(p) {
				return nil, errors.New("invalid WebSocket subprotocol")
			}
		}
		return Layer{Subprotocols: options.Subprotocols}, nil
	}})
}

type Options struct {
	Subprotocols []string `json:"subprotocols,omitempty"`
}
type Layer struct{ Subprotocols []string }

func (Layer) Scheme() string { return "ws" }
func (Layer) ValidateRoute(key string) error {
	if key == "*" {
		return nil
	}
	if !strings.HasPrefix(key, "/") || strings.ContainsAny(key, "?#\r\n ") {
		return fmt.Errorf("invalid WebSocket path %q", key)
	}
	return nil
}
func (l Layer) Serve(ctx context.Context, ln net.Listener, e *wire.Engine) error {
	upgrader := websocket.Upgrader{Subprotocols: l.Subprotocols}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !e.Has(r.URL.Path) {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer wire.ReleaseHijacked(r.Context())
		defer conn.Close()
		s := &session{conn: conn, request: r}
		err = e.Dispatch(r.URL.Path, s)
		code, reason := websocket.CloseNormalClosure, ""
		if err != nil {
			code, reason = websocket.CloseInternalServerErr, err.Error()
			var failure *closeError
			if errors.As(err, &failure) {
				code, reason = failure.code, failure.message
			}
		}
		// RFC 6455 close payload is at most 125 bytes, including its two-byte code.
		// Oversized script errors become a fixed protocol diagnostic, not truncation.
		if len(reason) > 123 || !utf8.ValidString(reason) {
			reason = "lmcode handler failed"
		}
		_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason))
	})
	p := new(http.Protocols)
	p.SetHTTP1(true)
	return wire.ServeHTTP(ctx, ln, handler, p)
}

type closeError struct {
	code    int
	message string
}

func (e *closeError) Error() string { return e.message }

type session struct {
	conn    *websocket.Conn
	request *http.Request
}

func (s *session) Receive() (string, string, error) {
	kind, b, err := s.conn.ReadMessage()
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return "", "", io.EOF
	}
	if err != nil {
		return "", "", err
	}
	if kind == websocket.BinaryMessage {
		return "binary", base64.StdEncoding.EncodeToString(b), nil
	}
	if !utf8.Valid(b) {
		return "", "", &closeError{websocket.CloseInvalidFramePayloadData, "invalid UTF-8 text"}
	}
	return "text", string(b), nil
}
func (s *session) Send(kind, body string) error {
	if kind == "binary" {
		b, err := base64.StdEncoding.Strict().DecodeString(body)
		if err != nil {
			return err
		}
		return s.conn.WriteMessage(websocket.BinaryMessage, b)
	}
	if kind != "" || !utf8.ValidString(body) {
		return errors.New("invalid WebSocket text frame")
	}
	return s.conn.WriteMessage(websocket.TextMessage, []byte(body))
}
func (s *session) Header(name string) string {
	if name == ":protocol" {
		return s.conn.Subprotocol()
	}
	return wire.HTTPHeader(s.request, name)
}
func (s *session) SetHeader(string, string) error {
	return errors.New("WebSocket handshake headers are already sent")
}
func (s *session) Fail(code int, message string) error {
	if !((code >= 3000 && code <= 4999) || code == 1000 || code == 1001 || code == 1002 || code == 1003 || code == 1007 || code == 1008 || code == 1009 || code == 1010 || code == 1011 || code == 1012 || code == 1013 || code == 1014) || len(message) > 123 || !utf8.ValidString(message) {
		return errors.New("invalid WebSocket close code/reason")
	}
	return &closeError{code, message}
}
