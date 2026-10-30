package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/voocel/litellm"
	"github.com/voocel/litellm/provider/compat"
	"github.com/voocel/litellm/provider/openai"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}
func main() {
	base := os.Getenv("LMMOCK_URL")
	if base == "" {
		panic("run under lmmock exec")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cp, err := compat.New(compat.Config{BaseURL: base + "/v1", APIKey: "fixture-key"})
	must(err)
	op, err := openai.New(openai.Config{BaseURL: base + "/v1", APIKey: "fixture-key", API: openai.APIResponses})
	must(err)
	for _, p := range []litellm.Provider{cp, op} {
		c, err := litellm.New(p)
		must(err)
		req := litellm.Request{Model: "mock-agent", Messages: []litellm.Message{litellm.UserText("ping")}}
		r, err := c.Chat(ctx, req)
		must(err)
		if r.Text() != "pong" {
			panic(r.Text())
		}
		stream, err := c.Stream(ctx, req)
		must(err)
		r, err = litellm.Collect(stream)
		must(err)
		must(stream.Close())
		if r.Text() != "pong" {
			panic(r.Text())
		}
		tool, err := litellm.NewTool("weather", "Weather", map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}, "required": []string{"city"}})
		must(err)
		req.Tools = []litellm.Tool{tool}
		r, err = c.Chat(ctx, req)
		must(err)
		if len(r.ToolCalls()) != 1 {
			panic("missing tool")
		}
		req.Messages = append(req.Messages, litellm.Assistant(r.Blocks...), litellm.ToolResultText("call_1", "Paris 21C"))
		r, err = c.Chat(ctx, req)
		must(err)
		if !strings.Contains(r.Text(), "Paris") {
			panic(r.Text())
		}
		fmt.Println("PASS Go LiteLLM " + p.Name() + " chat, stream and tool round trip")
	}
}
