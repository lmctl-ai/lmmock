package wire

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"testing"
)

type testLayer struct{}

func (testLayer) Scheme() string                                     { return "test" }
func (testLayer) ValidateRoute(string) error                         { return nil }
func (testLayer) Serve(context.Context, net.Listener, *Engine) error { return nil }
func init() {
	Register("fixture", Definition{SAM: "FixtureHandler", New: func(string, json.RawMessage) (Layer, error) { return testLayer{}, nil }})
}
func TestSchemaDeclaresProtocolSAM(t *testing.T) {
	b, err := Schema("fixture")
	if err != nil {
		t.Fatal("extension SAM schema unavailable:", err)
	}
	if !bytes.Contains(b, []byte("FixtureHandler")) {
		t.Fatal("empty protocol schema")
	}
}
