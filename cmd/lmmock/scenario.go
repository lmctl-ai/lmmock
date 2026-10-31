package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/lmctl-ai/lmmock/internal/scenario"
	"os"
	"path/filepath"
)

func scenarioCommand(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: lmmock scenario import|scrub|verify --input FLOWS [--output FILE | --target URL] [--exact]")
		return 2
	}
	f := flag.NewFlagSet("scenario", flag.ContinueOnError)
	input := f.String("input", "", "lminspect JSONL file")
	output := f.String("output", "", "private output file")
	target := f.String("target", "", "loopback mock origin")
	exact := f.Bool("exact", false, "preserve dynamic values; still scrub secrets")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *input == "" {
		return 2
	}
	file, e := os.Open(*input)
	if e != nil {
		return fail(fmt.Errorf("cannot open capture"))
	}
	defer file.Close()
	if args[0] == "verify" {
		if *output != "" || *exact || *target == "" {
			return 2
		}
		if e = scenario.Verify(file, *target); e != nil {
			return fail(e)
		}
		fmt.Println("FIDELITY OK")
		return 0
	}
	if (args[0] != "import" && args[0] != "scrub") || *output == "" || *target != "" {
		return 2
	}
	s, flows, e := scenario.Import(file, *exact)
	if e != nil {
		return fail(e)
	}
	// Do not create an output at all until conversion and residue scanning pass.
	out, e := os.CreateTemp(filepath.Dir(*output), ".scenario-*")
	if e != nil {
		return fail(fmt.Errorf("cannot write output"))
	}
	defer out.Close()
	defer os.Remove(out.Name())
	enc := json.NewEncoder(out)
	if args[0] == "import" {
		enc.SetIndent("", "  ")
		e = enc.Encode(s)
	} else {
		for _, flow := range flows {
			if e = enc.Encode(flow); e != nil {
				break
			}
		}
	}
	if e != nil {
		return fail(e)
	}
	if e = out.Close(); e != nil {
		return fail(e)
	}
	if e = os.Rename(out.Name(), *output); e != nil {
		return fail(fmt.Errorf("cannot replace output"))
	}
	return 0
}
