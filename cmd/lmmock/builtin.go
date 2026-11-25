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

//go:embed builtin/google-apis.lmcode
var builtinGoogleAPIs []byte

func builtinProgram(name string) ([]byte, error) {
	switch name {
	case "llm-api":
		return builtinLLMAPI, nil
	case "oauth-codex-device":
		return builtinOAuthCodexDevice, nil
	case "google-apis":
		return builtinGoogleAPIs, nil
	}
	return nil, fmt.Errorf("unknown builtin %q (available: llm-api, oauth-codex-device, google-apis)", name)
}
