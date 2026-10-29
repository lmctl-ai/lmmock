package proto

import (
	"context"
	"encoding/json"
	"net"
	"testing"
)

type stubServer struct{ scheme string }

func (s stubServer) Serve(ctx context.Context, ln net.Listener) error {
	<-ctx.Done()
	return ln.Close()
}
func (s stubServer) Scheme() string { return s.scheme }

func TestRegisterLookupNames(t *testing.T) {
	name := "stub-" + t.Name()
	Register(name, func(cfg json.RawMessage) (Server, error) {
		return stubServer{scheme: "stub"}, nil
	})
	f, ok := Lookup(name)
	if !ok || f == nil {
		t.Fatalf("Lookup(%q) failed", name)
	}
	srv, err := f(nil)
	if err != nil || srv.Scheme() != "stub" {
		t.Fatalf("factory returned %v, %v", srv, err)
	}
	found := false
	for _, n := range Names() {
		if n == name {
			found = true
		}
	}
	if !found {
		t.Fatalf("Names() missing %q: %v", name, Names())
	}
	if _, ok := Lookup("no-such-protocol"); ok {
		t.Fatal("Lookup succeeded for unregistered name")
	}
}

func TestRegisterPanics(t *testing.T) {
	for name, call := range map[string]func(){
		"empty name":  func() { Register("", func(json.RawMessage) (Server, error) { return nil, nil }) },
		"nil factory": func() { Register("nilfac-"+t.Name(), nil) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("Register did not panic")
				}
			}()
			call()
		})
	}
	dup := "dup-" + t.Name()
	factory := func(json.RawMessage) (Server, error) { return stubServer{}, nil }
	Register(dup, factory)
	defer func() {
		if recover() == nil {
			t.Fatal("duplicate Register did not panic")
		}
	}()
	Register(dup, factory)
}
