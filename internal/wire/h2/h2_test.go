package h2

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"github.com/lmctl-ai/lmmock/internal/wire/testutil"
	"io"
	"math/big"
	"net"
	"net/http"
	"reflect"
	"testing"
	"time"
)

type observer struct {
	header http.Header
	events []string
}

func (w *observer) Header() http.Header { return w.header }
func (w *observer) WriteHeader(int)     {}
func (w *observer) Write(b []byte) (int, error) {
	w.events = append(w.events, "write:"+string(b))
	return len(b), nil
}
func (w *observer) Flush() { w.events = append(w.events, "flush") }
func flushProbe(t *testing.T) {
	t.Helper()
	w := &observer{header: http.Header{}}
	s := &session{writer: w}
	if err := s.Send("", "event"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(w.events, []string{"write:event", "flush"}) {
		t.Fatal("event was not flushed before send returned:", w.events)
	}
}
func TestFlushBoundary(t *testing.T) { flushProbe(t) }

const script = `@events
fn events(s: Session) -> void implements HTTP2Handler {
 s.set_header("Content-Type","text/plain");s.send("ready\n");
 if (s.recv()) { s.send(s.body()); }
}
bind("POST /events","events");`

func TestHTTP2IncrementalDuplex(t *testing.T) {
	flushProbe(t) // A removed flush fails before a network client could wait for it.
	e, err := wire.New("http2", Layer{}, testutil.Compile(t, "http2", script), nil)
	if err != nil {
		t.Fatal(err)
	}
	address := testutil.Start(t, e)
	protocols := new(http.Protocols)
	protocols.SetUnencryptedHTTP2(true)
	tr := &http.Transport{Protocols: protocols}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	reader, writer := io.Pipe()
	defer reader.Close()
	defer writer.Close()
	req, err := http.NewRequest("POST", "http://"+address+"/events", reader)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 {
		t.Fatal("not HTTP/2", response.Proto)
	}
	lines := bufio.NewReader(response.Body)
	first, err := lines.ReadString('\n')
	if err != nil || first != "ready\n" {
		t.Fatal("first event", first, err)
	}
	if _, err = io.WriteString(writer, "client-after-ready\n"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	rest, err := io.ReadAll(lines)
	if err != nil || string(rest) != "client-after-ready\n" {
		t.Fatal("event order", string(rest), err)
	}
}
func TestHTTP2TLSListener(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "local mock"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: time.Unix(0, 0), NotAfter: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := tls.NewListener(raw, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, NextProtos: []string{"h2"}, MinVersion: tls.VersionTLS12})
	body := `@ok fn ok(s: Session) -> void implements HTTP2Handler { s.send("secure"); } bind("GET /","ok");`
	e, err := wire.New("http2", Layer{}, testutil.Compile(t, "http2", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.Serve(ctx, ln) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	tr := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}, ForceAttemptHTTP2: true}
	defer tr.CloseIdleConnections()
	res, err := (&http.Client{Transport: tr}).Get("https://" + raw.Addr().String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.ProtoMajor != 2 || string(b) != "secure" {
		t.Fatalf("TLS HTTP/2: %s %q %v", res.Proto, b, err)
	}
}

func TestStatusAndPseudoHeaders(t *testing.T) {
	body := `@meta fn meta(s: Session) -> void implements HTTP2Handler { s.status(201); s.send(s.header(":method")+" "+s.header(":path")+" "+s.header(":authority")); } bind("POST /meta","meta");`
	e, err := wire.New("http2", Layer{}, testutil.Compile(t, "http2", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	address := testutil.Start(t, e)
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	tr := &http.Transport{Protocols: p}
	defer tr.CloseIdleConnections()
	req, _ := http.NewRequest("POST", "http://"+address+"/meta?x=1", nil)
	res, err := (&http.Client{Transport: tr}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 201 || string(b) != "POST /meta?x=1 "+address {
		t.Fatalf("status/metadata: %d %q %v", res.StatusCode, b, err)
	}
}

func TestCatchAllAndHeaderRefusal(t *testing.T) {
	body := `@all fn all(s: Session) -> void implements HTTP2Handler { s.send(s.method()); } bind("*","all");`
	e, err := wire.New("http2", Layer{}, testutil.Compile(t, "http2", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := new(http.Protocols)
	p.SetUnencryptedHTTP2(true)
	tr := &http.Transport{Protocols: p}
	defer tr.CloseIdleConnections()
	res, err := (&http.Client{Transport: tr}).Get("http://" + testutil.Start(t, e) + "/new/service/123")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || string(b) != "GET /new/service/123" {
		t.Fatal(string(b), err)
	}
	w := &observer{header: http.Header{}}
	s := &session{writer: w}
	if s.SetHeader("bad(name)", "value") == nil || s.SetHeader("X-Good", "bad\x00value") == nil {
		t.Fatal("invalid header accepted")
	}
}
