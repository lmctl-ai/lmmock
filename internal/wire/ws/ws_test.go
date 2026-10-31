package ws

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/gorilla/websocket"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"github.com/lmctl-ai/lmmock/internal/wire/testutil"
	"net/http"
	"testing"
)

const ordered = `@chat
fn chat(s: Session) -> void implements WebSocketHandler {
 if (!s.recv() || s.frame() != "text" || s.body() != "hello") { s.fail(1008,"expected hello"); }
 s.send("ready");
 if (!s.recv() || s.body() != "go") { s.fail(1008,"expected go"); }
 s.send("done");
}
@binary
fn binary(s: Session) -> void implements WebSocketHandler { if (s.recv()) { s.send_binary(s.body()); } }
@counter
fn counter(s: Session) -> void implements WebSocketHandler { s.send(str(count("connections"))); }
bind("/chat","chat");bind("/binary","binary");bind("/count","counter");`

func host(t *testing.T, body string) (string, []byte) {
	t.Helper()
	code := testutil.Compile(t, "ws", body)
	e, err := wire.New("ws", Layer{}, code, nil)
	if err != nil {
		t.Fatal(err)
	}
	return "ws://" + testutil.Start(t, e), code
}
func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, _, err := (&websocket.Dialer{}).Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}
func TestOrderedWebSocketAndBinary(t *testing.T) {
	url, _ := host(t, ordered)
	c := dial(t, url+"/chat")
	for _, pair := range [][2]string{{"hello", "ready"}, {"go", "done"}} {
		if err := c.WriteMessage(websocket.TextMessage, []byte(pair[0])); err != nil {
			t.Fatal(err)
		}
		kind, b, err := c.ReadMessage()
		if err != nil || kind != websocket.TextMessage || string(b) != pair[1] {
			t.Fatalf("frame order: %d %q %v", kind, b, err)
		}
	}
	_, _, err := c.ReadMessage()
	if !websocket.IsCloseError(err, 1000) {
		t.Fatalf("normal close: %v", err)
	}
	binary := dial(t, url+"/binary")
	want := []byte{0, 255, 1}
	if err = binary.WriteMessage(websocket.BinaryMessage, want); err != nil {
		t.Fatal(err)
	}
	kind, b, err := binary.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage || !bytes.Equal(b, want) {
		t.Fatalf("binary: %d %x %v", kind, b, err)
	}
	for _, want := range []string{"1", "2"} {
		counter := dial(t, url+"/count")
		_, b, err := counter.ReadMessage()
		if err != nil || string(b) != want {
			t.Fatalf("shared state: %q %v", b, err)
		}
		counter.Close()
	}
}
func TestWrongFrameOrderFails(t *testing.T) {
	url, _ := host(t, ordered)
	c := dial(t, url+"/chat")
	if err := c.WriteMessage(websocket.TextMessage, []byte("go")); err != nil {
		t.Fatal(err)
	}
	_, _, err := c.ReadMessage()
	var close *websocket.CloseError
	if !errors.As(err, &close) || close.Code != 1008 || close.Text != "expected hello" {
		t.Fatalf("wrong order accepted: %v", err)
	}
}
func TestBindingAndSchemaRefusals(t *testing.T) {
	code := testutil.Compile(t, "ws", ordered)
	for name, mutated := range map[string][]byte{"hash": append([]byte(nil), code...), "SAM": bytes.Replace(code, []byte("WebSocketHandler"), []byte("BadSocketHandler"), 1)} {
		if name == "hash" {
			mutated[6] ^= 1
		}
		if _, err := wire.New("ws", Layer{}, mutated, nil); err == nil {
			t.Fatal("accepted", name)
		}
	}
	for _, body := range []string{ordered + `bind("/chat","chat");`, ordered + `bind("/missing","absent");`, ordered + `bind("bad","chat");`} {
		if _, err := wire.New("ws", Layer{}, testutil.Compile(t, "ws", body), json.RawMessage(`{}`)); err == nil {
			t.Fatal("accepted bad binding")
		}
	}
}
func TestUnboundPathDoesNotUpgrade(t *testing.T) {
	url, _ := host(t, ordered)
	_, r, err := (&websocket.Dialer{}).Dial(url+"/missing", nil)
	if err == nil || r.StatusCode != http.StatusNotFound {
		t.Fatal("unbound path upgraded", err, r)
	}
}

func TestRawFrameOrderAndSubprotocol(t *testing.T) {
	code := testutil.Compile(t, "ws", ordered)
	e, err := wire.New("ws", Layer{Subprotocols: []string{"fixture-v1"}}, code, nil)
	if err != nil {
		t.Fatal(err)
	}
	url := "ws://" + testutil.Start(t, e)
	d := &websocket.Dialer{Subprotocols: []string{"fixture-v1"}}
	c, _, err := d.Dial(url+"/chat", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if c.Subprotocol() != "fixture-v1" {
		t.Fatal("subprotocol was not negotiated")
	}
	// A masked continuation frame without a starting data frame is invalid RFC 6455.
	if _, err = c.UnderlyingConn().Write([]byte{0x80, 0x81, 1, 2, 3, 4, 'x' ^ 1}); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.ReadMessage()
	if !websocket.IsCloseError(err, 1002) {
		t.Fatal("invalid raw frame order accepted:", err)
	}
	_, response, err := d.Dial(url+"/chat", http.Header{"Origin": []string{"https://not-the-loopback-host.invalid"}})
	if err == nil || response.StatusCode != 403 {
		t.Fatal("cross-origin handshake accepted", err)
	}
}
func TestWildcardPolicyNeedsNoGoRoute(t *testing.T) {
	url, _ := host(t, `@any fn any(s: Session) -> void implements WebSocketHandler {s.send(s.method()+" "+s.header(":path"));} bind("*","any");`)
	c := dial(t, url+"/new-application/42?x=1")
	_, b, err := c.ReadMessage()
	if err != nil || string(b) != "/new-application/42 /new-application/42?x=1" {
		t.Fatal(string(b), err)
	}
}
