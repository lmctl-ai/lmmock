package mock

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// TLSOptions adds certificate identities, never listener addresses or DNS changes.
type TLSOptions struct{ Hosts []string }

type tlsMaterial struct {
	config            *tls.Config
	directory, caFile string
}

func makeTLS(options TLSOptions) (*tlsMaterial, error) {
	hosts := append([]string{"localhost", "127.0.0.1", "::1"}, options.Hosts...)
	dns := []string{}
	ips := []net.IP{}
	name := regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)
	for _, host := range hosts {
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
			continue
		}
		if !name.MatchString(host) || strings.Contains(host, "..") {
			return nil, errors.New("TLS host must be a literal DNS name or IP")
		}
		dns = append(dns, host)
	}
	serial := func() (*big.Int, error) { return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128)) }
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	caSerial, err := serial()
	if err != nil {
		return nil, err
	}
	leafSerial, err := serial()
	if err != nil {
		return nil, err
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "lmmock per-run test CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	leaf := &x509.Certificate{SerialNumber: leafSerial, Subject: pkix.Name{CommonName: "lmmock loopback"}, DNSNames: dns, IPAddresses: ips, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "lmmock-ca-")
	if err != nil {
		return nil, err
	}
	file := filepath.Join(dir, "ca.pem")
	if err = os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	// Private keys stay in memory; the only exported material is the public CA.
	config := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}, Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, caDER}, PrivateKey: leafKey}}}
	return &tlsMaterial{config: config, directory: dir, caFile: file}, nil
}
func listenLoopback(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("listen address must be a loopback IP (127.0.0.1 or ::1)")
	}
	return net.Listen("tcp", address)
}
func serveListener(e *Engine, listener net.Listener, scheme string) *Server {
	s := &Server{URL: scheme + "://" + listener.Addr().String(), http: &http.Server{Handler: e, DisableGeneralOptionsHandler: e.forwarder != nil, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}, done: make(chan error, 1)}
	go func() {
		err := s.http.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		s.done <- err
	}()
	return s
}
func StartTLS(e *Engine, address string, options TLSOptions) (*Server, error) {
	listener, err := listenLoopback(address)
	if err != nil {
		return nil, err
	}
	material, err := makeTLS(options)
	if err != nil {
		listener.Close()
		return nil, err
	}
	server := serveListener(e, tls.NewListener(listener, material.config), "https")
	server.CACert, server.caDirectory = material.caFile, material.directory
	return server, nil
}

// ClientEnv changes only the child invocation, never machine trust or the parent.
func (s *Server) ClientEnv(environment []string) []string {
	overrides := map[string]string{"LMMOCK_URL": s.URL}
	if s.CACert != "" {
		for _, key := range []string{"LMMOCK_CA_CERT", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
			overrides[key] = s.CACert
		}
	}
	result := make([]string, 0, len(environment)+len(overrides))
	for _, entry := range environment {
		key := strings.SplitN(entry, "=", 2)[0]
		replace := false
		for wanted := range overrides {
			if strings.EqualFold(key, wanted) {
				replace = true
				break
			}
		}
		if !replace {
			result = append(result, entry)
		}
	}
	for _, key := range []string{"LMMOCK_URL", "LMMOCK_CA_CERT", "SSL_CERT_FILE", "NODE_EXTRA_CA_CERTS", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE"} {
		if value, ok := overrides[key]; ok {
			result = append(result, key+"="+value)
		}
	}
	return result
}
