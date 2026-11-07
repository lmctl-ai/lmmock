package rpcstdio

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func runConversation(t *testing.T, alias, input string) (string, error) {
	t.Helper()
	cfg, e := os.ReadFile("../../tests/stdio/acp-conversation.json")
	if e != nil {
		t.Fatal(e)
	}
	code, e := Preset("acp")
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	engine, e := New(code, Options{Config: cfg, Alias: alias}, NewWire(strings.NewReader(input), &out, "line"))
	if e != nil {
		t.Fatal(e)
	}
	if e = engine.Start(map[string]any{}); e != nil {
		return out.String(), e
	}
	e = engine.Run()
	return out.String(), e
}

func chatPrompt(id, text string) string {
	return rpc(id, "session/prompt", map[string]any{"sessionId": "acp-conversation-1", "prompt": []any{map[string]any{"type": "text", "text": text}}})
}

func chunkTexts(msgs []map[string]any) (chunks []string, replies map[any]map[string]any) {
	replies = map[any]map[string]any{}
	for _, m := range msgs {
		if m["method"] == "session/update" {
			u := m["params"].(map[string]any)["update"].(map[string]any)
			chunks = append(chunks, u["content"].(map[string]any)["text"].(string))
		} else if id, ok := m["id"]; ok {
			replies[id] = m
		}
	}
	return
}

// A client can hold one session open and take several turns: each prompt streams
// its scripted chunks in order and ends with its own stopReason.
func TestACPMultiTurnConversation(t *testing.T) {
	in := openACP() + chatPrompt("t1", "Hello agent") + chatPrompt("t2", "What is the weather?") + chatPrompt("t3", "ok bye")
	out, e := runConversation(t, "Chat", in)
	if e != nil {
		t.Fatal(e, out)
	}
	msgs := decodeLines(t, out)
	chunks, replies := chunkTexts(msgs)
	want := []string{"Hi there! ", "I am the mock ACP agent.", "It is sunny in the fixture world.", "Goodbye", "!"}
	if strings.Join(chunks, "|") != strings.Join(want, "|") {
		t.Fatalf("chunks %q, want %q", chunks, want)
	}
	for _, id := range []string{"t1", "t2", "t3"} {
		r := replies[id]["result"].(map[string]any)
		if r["stopReason"] != "end_turn" {
			t.Fatalf("%s: %v", id, r)
		}
	}
	// Streaming order: every turn's chunks precede that turn's reply.
	last := ""
	for _, m := range msgs {
		if id, ok := m["id"].(string); ok && strings.HasPrefix(id, "t") {
			if last != "" && id <= last {
				t.Fatalf("replies out of order: %s after %s", id, last)
			}
			last = id
		}
	}
}

func TestACPStopReasonIsScripted(t *testing.T) {
	out, e := runConversation(t, "Truncated", openACP()+chatPrompt("p", "anything"))
	if e != nil {
		t.Fatal(e, out)
	}
	_, replies := chunkTexts(decodeLines(t, out))
	if replies["p"]["result"].(map[string]any)["stopReason"] != "max_tokens" {
		t.Fatal(out)
	}
}

// A turn whose prompt does not contain the scripted expectation fails loudly
// instead of replying, so a client that mangles history is caught.
func TestACPUnexpectedPromptFails(t *testing.T) {
	_, e := runConversation(t, "Chat", openACP()+chatPrompt("t1", "something else entirely"))
	if e == nil {
		t.Fatal("prompt that violates the cell expectation was answered")
	}
}

// Running past the last scripted turn is an error, not a default reply.
func TestACPExhaustedScriptFails(t *testing.T) {
	in := openACP() + chatPrompt("t1", "Hello") + chatPrompt("t2", "weather") + chatPrompt("t3", "bye") + chatPrompt("t4", "more")
	_, e := runConversation(t, "Chat", in)
	if e == nil {
		t.Fatal("fourth turn answered although only three are scripted")
	}
}

func TestACPScriptedErrorTurn(t *testing.T) {
	out, e := runConversation(t, "Failing", openACP()+chatPrompt("a", "one")+chatPrompt("b", "two"))
	if e == nil {
		t.Fatal("scripted error turn did not stop the agent")
	}
	msgs := decodeLines(t, out)
	_, replies := chunkTexts(msgs)
	if replies["a"]["result"] == nil {
		t.Fatal(out)
	}
	errObj, _ := replies["b"]["error"].(map[string]any)
	if errObj == nil || errObj["message"] != "scripted agent failure" {
		t.Fatal(out)
	}
}
