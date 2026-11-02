package main

import (
	_ "embed"
	"fmt"
)

// Bytecode compiled from examples/*.lms by scripts/compile-builtin.sh.
//
//go:embed builtin/llm-api.lmcode
var builtinLLMAPI []byte

//go:embed builtin/oauth-codex-device.lmcode
var builtinOAuthCodexDevice []byte

func builtinProgram(name string) ([]byte, error) {
	switch name {
	case "llm-api":
		return builtinLLMAPI, nil
	case "oauth-codex-device":
		return builtinOAuthCodexDevice, nil
	}
	return nil, fmt.Errorf("unknown builtin %q (available: llm-api, oauth-codex-device)", name)
}
