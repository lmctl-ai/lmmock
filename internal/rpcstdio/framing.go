// Package rpcstdio owns stdio framing and annotated lmcode dispatch, not ACP/MCP policy.
package rpcstdio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type Wire struct {
	in   *bufio.Reader
	out  io.Writer
	mode string
}

func NewWire(in io.Reader, out io.Writer, mode string) *Wire {
	return &Wire{bufio.NewReader(in), out, mode}
}
func (w *Wire) Read() ([]byte, error) {
	if w.mode == "line" {
		for {
			b, e := w.in.ReadBytes('\n')
			if e != nil && len(b) == 0 {
				return nil, e
			}
			if len(bytes.TrimSpace(b)) != 0 {
				return bytes.TrimSpace(b), nil
			}
		}
	}
	n := -1
	for {
		line, e := w.in.ReadString('\n')
		if e != nil {
			return nil, e
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			break
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("invalid frame header")
		}
		if strings.EqualFold(k, "Content-Length") {
			if n >= 0 {
				return nil, fmt.Errorf("duplicate Content-Length")
			}
			var err error
			n, err = strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("invalid Content-Length")
			}
		}
	}
	if n < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	b := make([]byte, n)
	_, e := io.ReadFull(w.in, b)
	return b, e
}
func (w *Wire) Write(b []byte) error {
	var frame []byte
	if w.mode == "line" {
		frame = append(append([]byte{}, b...), '\n')
	} else {
		frame = append([]byte(fmt.Sprintf("Content-Length: %d\r\n\r\n", len(b))), b...)
	}
	return w.writeBytes(frame)
}
func (w *Wire) writeBytes(frame []byte) error {
	n, e := w.out.Write(frame)
	if e == nil && n != len(frame) {
		e = io.ErrShortWrite
	}
	if e != nil {
		return e
	}
	if f, ok := w.out.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}
