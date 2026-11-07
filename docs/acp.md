# ACP server mock

`lmmock stdio acp` is a scripted [Agent Client Protocol](https://agentclientprotocol.com)
agent. It speaks newline-delimited JSON-RPC 2.0 on stdin/stdout, so any ACP client
(an editor, `lmconn acp chat`, your own harness) can spawn it as the agent
subprocess. No compiler, network or API key is needed.

```sh
lmmock stdio acp --scenario tests/stdio/acp-conversation.json --alias Chat
```

## Conversation script

Replies are scripted per `(alias, turn)`. A session takes as many `session/prompt`
turns as the script has cells for; turn counters start at 1 and belong to the
process (use `--state DIR` to persist them across processes).
[`tests/stdio/acp-conversation.json`](../tests/stdio/acp-conversation.json) is a
three-turn example:

```json
{ "alias": "Chat", "turn": 1, "expect": "Hello",
  "text": "Hi there! I am the mock ACP agent.",
  "chunks": ["Hi there! ", "I am the mock ACP agent."] }
```

| Field | Meaning |
|---|---|
| `expect` | Substring that must appear in the prompt params, else the agent aborts (catches clients that drop or mangle the turn). |
| `text` | Required reply text. |
| `chunks` | Optional. Streamed as separate `agent_message_chunk` updates in order; `text` is used as one chunk without it. |
| `stopReason` | Optional, default `end_turn` (e.g. `max_tokens`, `refusal`). |
| `error` | Reply with a JSON-RPC error (-32000) and stop instead of text. |

Running past the last scripted turn, or a duplicate cell, fails loudly: there is no
default reply.

## Wire flow

```
client                         agent (lmmock)
  initialize {protocolVersion:1} ->        <- {protocolVersion:1, agentCapabilities}
  notifications/initialized ->
  session/new {cwd, mcpServers} ->         <- {sessionId}
  session/prompt {sessionId, prompt} ->    <- session/update agent_message_chunk (x N)
                                           <- {stopReason}
  session/prompt ... (turn 2, 3, ...)
```

Also supported: `session/load` (known fixture session only),
`session/set_mode`, `session/set_model`, `session/set_config_option`.
Cancellation, permission requests and fs/terminal client calls are not simulated
by the stock preset; see [ACP provider-profile coverage](../README.md#acp-provider-profile-coverage).

## Client side

The matching client is `lmconn acp chat` in
[lmctl-ai/lmconn](https://github.com/lmctl-ai/lmconn); its integration test drives
this mock through a multi-turn conversation.

Tests for the conversation script live in
`internal/rpcstdio/acp_conversation_test.go`.
