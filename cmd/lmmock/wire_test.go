package main

import (
	"context"
	"encoding/json"
	"github.com/gorilla/websocket"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"github.com/lmctl-ai/lmmock/internal/wire/testutil"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestWireClientProcess(t *testing.T) {
	if os.Getenv("LMMOCK_WIRE_CLIENT") != "1" {
		return
	}
	c, _, err := (&websocket.Dialer{}).Dial(os.Getenv("LMMOCK_URL")+"/echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err = c.WriteMessage(websocket.TextMessage, []byte("client")); err != nil {
		t.Fatal(err)
	}
	_, b, err := c.ReadMessage()
	if err != nil || string(b) != "client" {
		t.Fatal(string(b), err)
	}
	if err = os.WriteFile(os.Getenv("LMMOCK_WIRE_RESULT"), []byte(os.Getenv("LMMOCK_ADDR")), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestWireExecOwnsListenerAndClient(t *testing.T) {
	dir := t.TempDir()
	body := `@echo fn echo(s: Session) -> void implements WebSocketHandler { if(s.recv()){s.send(s.body());} } bind("/echo","echo");`
	code := testutil.Compile(t, "ws", body)
	os.WriteFile(filepath.Join(dir, "program.dslb"), code, 0600)
	config := filepath.Join(dir, "scenario.json")
	b, _ := json.Marshal(wire.Config{Program: "program.dslb"})
	os.WriteFile(config, b, 0600)
	result := filepath.Join(dir, "result")
	t.Setenv("LMMOCK_WIRE_CLIENT", "1")
	t.Setenv("LMMOCK_WIRE_RESULT", result)
	if code := wireCommand(context.Background(), []string{"exec", "ws", "--config", config, "--", os.Args[0], "-test.run=^TestWireClientProcess$"}); code != 0 {
		t.Fatal("exec failed", code)
	}
	address, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := net.Dial("tcp", string(address)); err == nil {
		c.Close()
		t.Fatal("listener remained open after client exit")
	}
}
func TestWireConfigurationRefusals(t *testing.T) {
	if code := wireCommand(context.Background(), []string{"serve", "ws", "--config", "missing", "--listen", "0.0.0.0:0"}); code == 0 {
		t.Fatal("non-loopback listener accepted")
	}
	for _, args := range [][]string{{"exec", "ws"}, {"serve", "unknown", "--config", "missing"}, {"serve", "ws", "--config", "missing", "unexpected"}} {
		if wireCommand(context.Background(), args) == 0 {
			t.Fatal("bad CLI accepted", args)
		}
	}
}
