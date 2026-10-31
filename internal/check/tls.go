package check

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"os"
)

// Go's platform verifier does not use SSL_CERT_FILE uniformly (notably Windows).
// An exec child can explicitly trust this one per-run public CA on any OS.
func clientTransport() (*http.Transport, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if file := os.Getenv("LMMOCK_CA_CERT"); file != "" {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, errors.New("cannot read LMMOCK_CA_CERT")
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(raw) {
			return nil, errors.New("invalid LMMOCK_CA_CERT")
		}
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
	}
	return transport, nil
}
