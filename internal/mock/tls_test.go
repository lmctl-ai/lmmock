package mock

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestTLSPerRunCAAndFixedHTTPSURL(t *testing.T) {
	engine, err := New(fixture(t, "streaming"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := StartTLS(engine, "127.0.0.1:0", TLSOptions{Hosts: []string{"fixed.mock.test"}})
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	raw, err := os.ReadFile(server.CACert)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "PRIVATE KEY") {
		t.Fatal("exported CA private key")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		t.Fatal("invalid public CA export")
	}
	address := strings.TrimPrefix(server.URL, "https://")
	for _, trust := range []bool{false, true} {
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}}
		if trust {
			transport.TLSClientConfig.RootCAs = pool
		}
		client := &http.Client{Transport: transport}
		// This URL never changes to the listener URL: connection routing is separate
		// from HTTPS hostname verification and the exported CA trust decision.
		response, err := client.Get("https://fixed.mock.test/chunks")
		if !trust {
			if err == nil {
				response.Body.Close()
				t.Fatal("HTTPS passed without exported CA")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		transport.CloseIdleConnections()
		if err != nil || string(body) != "alphabeta" {
			t.Fatalf("trusted fixed HTTPS failed: %q %v", body, err)
		}
	}
	second, err := StartTLS(engine, "127.0.0.1:0", TLSOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	other, err := os.ReadFile(second.CACert)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == string(other) {
		t.Fatal("reused CA across runs")
	}
	file := server.CACert
	if err = server.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("CA export survived close")
	}
	if err = server.Close(); err != nil {
		t.Fatal("repeated close", err)
	}
}
func TestTLSChildEnvironmentIsScoped(t *testing.T) {
	server := &Server{URL: "https://127.0.0.1:1", CACert: "/private CA/ca.pem"}
	original := []string{"LMMOCK_URL=old", "ssl_cert_file=old", "NODE_EXTRA_CA_CERTS=old", "KEEP=yes"}
	env := server.ClientEnv(original)
	values := map[string]string{}
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		values[strings.ToUpper(k)] = v
	}
	for _, key := range []string{"LMMOCK_CA_CERT", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		if values[key] != server.CACert {
			t.Fatal("missing CA export", key)
		}
	}
	if values["LMMOCK_URL"] != server.URL || values["KEEP"] != "yes" || original[0] != "LMMOCK_URL=old" {
		t.Fatal("environment leak/mutation")
	}
	if _, err := makeTLS(TLSOptions{Hosts: []string{"https://wrong"}}); err == nil {
		t.Fatal("accepted URL as certificate identity")
	}
}
