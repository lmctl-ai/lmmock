package check

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestExportedCAIsRequired(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer server.Close()
	file := t.TempDir() + "/ca.pem"
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LMMOCK_CA_CERT", "")
	transport, err := clientTransport()
	if err != nil {
		t.Fatal(err)
	}
	response, err := (&http.Client{Transport: transport}).Get(server.URL)
	if err == nil {
		response.Body.Close()
		t.Fatal("HTTPS passed without exported CA")
	}
	transport.CloseIdleConnections()
	t.Setenv("LMMOCK_CA_CERT", file)
	transport, err = clientTransport()
	if err != nil {
		t.Fatal(err)
	}
	response, err = (&http.Client{Transport: transport}).Get(server.URL)
	if err != nil {
		t.Fatal("exported CA was ignored", err)
	}
	response.Body.Close()
	transport.CloseIdleConnections()
}
