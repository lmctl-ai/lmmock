package main

import (
	"context"
	"testing"
)

func TestProtoRejectsNonLoopbackAndExtraArguments(t *testing.T) {
	for _, args := range [][]string{
		{"redis", "--listen", "0.0.0.0:0"},
		{"redis", "--listen", ":0"},
		{"redis", "--listen", "[::]:0"},
		{"redis", "--listen", "localhost:0"},
		{"redis", "--listen", "192.0.2.1:0"},
		{"redis", "unexpected"},
	} {
		t.Run(args[len(args)-1], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if code := protoCommand(ctx, args); code != 2 {
				t.Fatalf("exit = %d; want 2", code)
			}
		})
	}
}
