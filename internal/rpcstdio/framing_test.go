package rpcstdio

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestFraming(t *testing.T) {
	for _, mode := range []string{"line", "content-length"} {
		var b bytes.Buffer
		w := NewWire(strings.NewReader(""), &b, mode)
		if e := w.Write([]byte(`{"id":"雪","result":{}}`)); e != nil {
			t.Fatal(e)
		}
		r := NewWire(&b, io.Discard, mode)
		got, e := r.Read()
		if e != nil || string(got) != `{"id":"雪","result":{}}` {
			t.Fatal(string(got), e)
		}
	}
}
func TestFramingRefusals(t *testing.T) {
	for _, raw := range []string{"Content-Length: 5\r\nContent-Length: 5\r\n\r\n12345", "Content-Length: -1\r\n\r\n", "Content-Length: 10\r\n\r\n{}"} {
		if _, e := NewWire(strings.NewReader(raw), io.Discard, "content-length").Read(); e == nil {
			t.Fatal("bad frame accepted")
		}
	}
}
