package main

import (
	_ "embed"
	"fmt"
)

// Bytecode compiled from examples/llm-api.lms by scripts/compile-builtin.sh.
//
//go:embed builtin/llm-api.dslb
var builtinLLMAPI []byte

func builtinProgram(name string) ([]byte, error) {
	if name == "llm-api" {
		return builtinLLMAPI, nil
	}
	return nil, fmt.Errorf("unknown builtin %q (available: llm-api)", name)
}
