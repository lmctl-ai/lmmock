package mock

import (
	"errors"
	"io"
	"net/http"

	lmcode "github.com/lmctl-ai/lmcode-go"
)

type exchangeMethod func(string, []string, string, func(*exchange, []lmcode.Value) (lmcode.Value, error))

// A stream writes while the Handler is running. No timer, worker or event queue
// separates a successful flush from the next lmcode instruction.
type responseStream struct {
	writer    http.ResponseWriter
	committed bool
	closed    bool
}

func (s *responseStream) begin(x *exchange) error {
	if s.closed {
		return errors.New("response stream is closed")
	}
	if s.committed {
		return nil
	}
	for key, values := range x.headers {
		s.writer.Header()[key] = append([]string(nil), values...)
	}
	s.writer.Header().Del("Content-Length")
	s.writer.Header().Del("Transfer-Encoding")
	s.writer.WriteHeader(x.status)
	s.committed = true
	return nil
}
func (s *responseStream) event(x *exchange, event string) error {
	if err := s.begin(x); err != nil {
		return err
	}
	if _, err := io.WriteString(s.writer, event); err != nil {
		return err
	}
	return http.NewResponseController(s.writer).Flush()
}
func (s *responseStream) fault(x *exchange, kind string) error {
	switch kind {
	case "disconnect", "truncated-chunk", "malformed-chunk":
	default:
		return errors.New("stream fault must be disconnect, truncated-chunk or malformed-chunk")
	}
	if x.request.ProtoMajor != 1 {
		return errors.New("raw chunk faults require HTTP/1")
	}
	if err := s.begin(x); err != nil {
		return err
	}
	if err := http.NewResponseController(s.writer).Flush(); err != nil {
		return err
	}
	conn, buffer, err := http.NewResponseController(s.writer).Hijack()
	if err != nil {
		return err
	}
	s.closed = true
	markStreamFailure(s.writer, kind)
	defer conn.Close()
	switch kind {
	case "truncated-chunk":
		_, err = buffer.WriteString("10\r\ncut")
	case "malformed-chunk":
		_, err = buffer.WriteString("not-a-chunk\r\n")
	}
	if err == nil {
		err = buffer.Flush()
	}
	return err
}
func streamMethods(method exchangeMethod) {
	method("stream_event", []string{"string"}, "void", func(x *exchange, args []lmcode.Value) (lmcode.Value, error) {
		if x.responseQueue != "" {
			enqueueResponse(x, "E"+args[0].Str)
			return lmcode.None(), nil
		}
		return lmcode.None(), x.stream.event(x, args[0].Str)
	})
	method("stream_fault", []string{"string"}, "void", func(x *exchange, args []lmcode.Value) (lmcode.Value, error) {
		return lmcode.None(), x.stream.fault(x, args[0].Str)
	})
}
