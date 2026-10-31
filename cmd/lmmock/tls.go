package main

import (
	"errors"
	"flag"

	"github.com/lmctl-ai/lmmock/internal/mock"
)

type tlsFlags struct {
	enabled *bool
	hosts   tlsHosts
}
type tlsHosts []string

func (h *tlsHosts) String() string { return "" }
func (h *tlsHosts) Set(value string) error {
	if value == "" {
		return errors.New("empty TLS host")
	}
	*h = append(*h, value)
	return nil
}
func addTLSFlags(flags *flag.FlagSet) *tlsFlags {
	result := &tlsFlags{enabled: flags.Bool("tls", false, "serve HTTPS with a fresh per-run CA")}
	flags.Var(&result.hosts, "tls-host", "additional certificate DNS name or IP (repeatable)")
	return result
}
func (flags *tlsFlags) start(engine *mock.Engine, address string) (*mock.Server, error) {
	if !*flags.enabled {
		if len(flags.hosts) > 0 {
			return nil, errors.New("--tls-host requires --tls")
		}
		return mock.Start(engine, address)
	}
	return mock.StartTLS(engine, address, mock.TLSOptions{Hosts: flags.hosts})
}
