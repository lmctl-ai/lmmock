package main

import (
	"flag"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/rpcstdio"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func guardedCommand(command, dir string) (string, error) {
	if dir == "" {
		return "", fmt.Errorf("--fake-bin is required for an owned peer command")
	}
	p, e := exec.LookPath(command)
	if e != nil {
		return "", e
	}
	p, e = filepath.EvalSymlinks(p)
	if e != nil {
		return "", e
	}
	root, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return "", e
	}
	root, _ = filepath.Abs(root)
	p, _ = filepath.Abs(p)
	rel, e := filepath.Rel(root, p)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("refusing peer command outside fake-bin")
	}
	return p, nil
}
func stdioCommand(args []string) int {
	if len(args) == 1 && args[0] == "--schema" {
		os.Stdout.Write(rpcstdio.Schema())
		return 0
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lmmock stdio acp|mcp|mcp-client|agent --scenario FILE [--preset NAME] [--lmcode FILE] [--alias NAME] [--state DIR] [--framing line|content-length] [--fake-bin DIR -- COMMAND]")
		return 2
	}
	kind := args[0]
	if kind != "acp" && kind != "mcp" && kind != "mcp-client" && kind != "agent" {
		return 2
	}
	f := flag.NewFlagSet("stdio", flag.ContinueOnError)
	scenario := f.String("scenario", "", "explicit cells/config JSON")
	preset := f.String("preset", "", "agent preset: claude, codex, agy, gemini")
	codePath := f.String("lmcode", "", "custom annotated handler bytecode")
	alias := f.String("alias", "", "cell alias, defaults to cwd basename")
	state := f.String("state", "", "private persistent counter directory")
	framing := f.String("framing", "line", "line or content-length")
	fakebin := f.String("fake-bin", "", "owned peer executable directory")
	stdinPrompt := f.Bool("stdin-prompt", false, "read one-shot agent prompt from stdin")
	if f.Parse(args[1:]) != nil || *scenario == "" || (*framing != "line" && *framing != "content-length") {
		return 2
	}
	name := kind
	if kind == "agent" {
		name = *preset
		if name != "claude" && name != "codex" && name != "agy" && name != "gemini" {
			return 2
		}
	} else if *preset != "" || *stdinPrompt {
		return 2
	}
	if kind != "mcp-client" && *fakebin != "" {
		return 2
	}
	if kind == "agent" && name != "codex" && *framing != "line" {
		return 2
	}
	if name == "codex" && *stdinPrompt {
		return 2
	}
	cfg, e := os.ReadFile(*scenario)
	if e != nil {
		return fail(e)
	}
	var code []byte
	if *codePath != "" {
		code, e = os.ReadFile(*codePath)
	} else {
		code, e = rpcstdio.Preset(name)
	}
	if e != nil {
		return fail(e)
	}
	cwd, e := os.Getwd()
	if e != nil {
		return fail(e)
	}
	if *alias == "" {
		*alias = filepath.Base(cwd)
	}
	var in io.Reader = os.Stdin
	var out io.Writer = os.Stdout
	var child *exec.Cmd
	var childIn io.WriteCloser
	var childOut io.ReadCloser
	if kind == "mcp-client" {
		if len(f.Args()) == 0 {
			return 2
		}
		path, e := guardedCommand(f.Args()[0], *fakebin)
		if e != nil {
			return fail(e)
		}
		child = exec.Command(path, f.Args()[1:]...)
		child.Stderr = os.Stderr
		childIn, e = child.StdinPipe()
		if e != nil {
			return fail(e)
		}
		childOut, e = child.StdoutPipe()
		if e != nil {
			return fail(e)
		}
		if e = child.Start(); e != nil {
			return fail(e)
		}
		in, out = childOut, childIn
		defer func() {
			childIn.Close()
			childOut.Close()
			if child.ProcessState == nil {
				_ = child.Process.Kill()
				_ = child.Wait()
			}
		}()
	}
	engine, e := rpcstdio.New(code, rpcstdio.Options{Alias: *alias, StateDir: *state, Config: cfg}, rpcstdio.NewWire(in, out, *framing))
	if e != nil {
		return fail(e)
	}
	input := ""
	if *stdinPrompt {
		b, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fail(err)
		}
		input = string(b)
	}
	if e = engine.Start(map[string]any{"argv": f.Args(), "cwd": cwd, "stdin": input}); e != nil {
		return fail(e)
	}
	if kind == "agent" && name != "codex" {
		return 0
	}
	if e = engine.Run(); e != nil {
		return fail(e)
	}
	if child != nil {
		childIn.Close()
		childOut.Close()
		if e = child.Wait(); e != nil {
			return fail(e)
		}
	}
	return 0
}
