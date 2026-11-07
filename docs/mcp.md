# MCP server mock (stdio)

`lmmock stdio mcp` is a scripted [Model Context Protocol](https://modelcontextprotocol.io)
server (protocol `2025-11-25`) on stdin/stdout, newline-delimited JSON-RPC. No
network, compiler or API key: an MCP client spawns it as a subprocess.

```sh
lmmock stdio mcp --scenario tests/stdio/mcp-conversation.json --alias Tools
```

## Surface

| Method | Behavior |
|---|---|
| `initialize` | Requires `protocolVersion` `2025-11-25`; advertises tools, resources, prompts. |
| `notifications/initialized` | Required before `tools/call`. |
| `tools/list` | One tool, `fixture` (any object arguments). |
| `tools/call` | Consumes the next scripted `(alias, turn)` cell: emits a `notifications/message` log, then `{content:[{type:"text",text}],isError:false}`. An `error` cell replies with JSON-RPC error -32000 and stops the server. |
| `resources/list`, `resources/read` | `fixture:///note` → `fixture note`. |
| `resources/templates/list` | Empty. |
| `prompts/list`, `prompts/get` | `fixture` → one user message `fixture prompt`. |
| `ping` | `{}` |

Unknown resources and prompts fail with -32602; an unknown tool name is an unscripted method (-32099) and the server stops. `expect` (substring of the
call params), duplicate/missing cells and exhausted scripts abort loudly; there is
no default reply. Paging, sampling, elicitation and subscriptions are not claimed.

## Multi-call script

[`tests/stdio/mcp-conversation.json`](../tests/stdio/mcp-conversation.json) scripts
four calls in one session (three results, then an error):

```json
{ "alias": "Tools", "turn": 1, "expect": "berlin", "text": "Berlin: 12C, cloudy" }
```

Covered by `internal/rpcstdio/mcp_conversation_test.go`. The matching stdio client
is `lmconn mcp` in [lmctl-ai/lmconn](https://github.com/lmctl-ai/lmconn). lmmock's
own `stdio mcp-client` is the reverse direction: a scripted client that tests an
MCP server.
