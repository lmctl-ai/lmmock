package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Native executable copies preserve real argv/process behavior on Windows too.
// The private manifest maps arbitrary executable names to normal lmmock modes;
// it contains no provider switches and never falls back to an installed CLI.
func processAlias(args []string) ([]string, error) {
	file := os.Getenv("LMMOCK_PROCESS_BINDINGS")
	if file == "" {
		return args, nil
	}
	var manifest struct {
		Version   int                 `json:"version"`
		Directory string              `json:"directory"`
		Aliases   map[string][]string `json:"aliases"`
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return nil, errors.New("invalid process bindings")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSuffix(filepath.Base(exe), ".exe")
	prefix, ok := manifest.Aliases[name]
	if !ok {
		return args, nil
	}
	root, err := filepath.EvalSymlinks(manifest.Directory)
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return nil, err
	}
	relative, err := filepath.Rel(root, real)
	if manifest.Version != 1 || err != nil || filepath.IsAbs(relative) || strings.ContainsAny(relative, "/\\") || relative == ".." || len(prefix) == 0 {
		return nil, fmt.Errorf("fake process escaped owned executable directory")
	}
	return append(append([]string(nil), prefix...), args...), nil
}
