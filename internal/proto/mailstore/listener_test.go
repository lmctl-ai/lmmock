package mailstore

import (
	"net"
	"testing"
	"time"
)

func TestTrackedListenerClosesAcceptedSocket(t *testing.T) {
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := TrackListener(raw)
	defer ln.Close()
	client, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("accepted socket still open")
	} else if e, ok := err.(net.Error); ok && e.Timeout() {
		t.Fatal("read timed out instead of closing")
	}
}
