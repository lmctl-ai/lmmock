// Package proto registers non-HTTP protocol mock servers (redis, mqtt, ...).
//
// Each protocol lives in internal/proto/<name> and self-registers from init:
//
//	func init() { proto.Register("redis", New) }
//
// cmd/lmmock/proto_imports.go blank-imports every protocol package.
package proto

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sort"
	"sync"
)

// Server serves one protocol on ln until ctx is cancelled. Serve must
// close ln before returning.
type Server interface {
	Serve(ctx context.Context, ln net.Listener) error
	// Scheme names the URL scheme reported in readiness JSON (e.g. "redis").
	Scheme() string
}

// Factory builds a Server from an optional JSON config document
// (nil when the user passed no --config).
type Factory func(cfg json.RawMessage) (Server, error)

var (
	mu       sync.RWMutex
	registry = map[string]Factory{}
)

// Register adds a protocol factory. It panics on an empty name, a nil
// factory, or a duplicate registration.
func Register(name string, f Factory) {
	if name == "" || f == nil {
		panic("proto: Register with empty name or nil factory")
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[name]; dup {
		panic("proto: Register called twice for " + name)
	}
	registry[name] = f
}

// Lookup returns the factory registered under name.
func Lookup(name string) (Factory, bool) {
	mu.RLock()
	defer mu.RUnlock()
	f, ok := registry[name]
	return f, ok
}

// Names returns the registered protocol names, sorted.
func Names() []string {
	mu.RLock()
	defer mu.RUnlock()
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ConfigError reports an invalid --config document.
func ConfigError(format string, args ...any) error {
	return fmt.Errorf("config: "+format, args...)
}
