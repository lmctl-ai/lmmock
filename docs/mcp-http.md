# Scriptable MCP HTTP mock

`examples/mcp/server.lms` exposes loopback-only protocol fixtures that run inside
`lmmock exec`. The transport revisions are deliberately separated:

- `examples/mcp/streamable-http/config.json` defaults to the current, stateless
  Streamable HTTP version, 2026-07-28. It accepts `POST /mcp`, requires
  `MCP-Protocol-Version`, `Mcp-Method`, the matching version in request `_meta`,
  and both media types in `Accept`. JSON and request-scoped SSE replies can be
  selected with `X-Mock-Reply-Mode: json|sse` or `POST /__mock/reply-mode`.
- `tests/mcp/session-config.json` selects the older Streamable HTTP revision
  2025-11-25 for session tests. It supports initialize/initialized,
  `Mcp-Session-Id`, GET event streams, DELETE, expiry, 404 for missing sessions,
  and cancellation notifications. Its GET stream retains the latest mock
  list-changed event and replays it when `Last-Event-ID` is older.
- `examples/mcp/http-sse/config.json` and the same script provide the deprecated
  2024-11-05 HTTP+SSE shape: `GET /sse`, its `endpoint` event, then
  `POST /messages?session_id=...`; JSON-RPC replies and progress are sent as
  `message` events.

The current mock offers `echo`, numeric `add`, cooperative `slow`, `fail`,
`crash`, and `delete_everything`. The last tool only increments a local call
record. Resources include text and base64 blob examples plus a resource template;
the `greet` prompt takes a `name`. `tools/list` advertises JSON input/output
schemas and read-only, destructive, idempotent, and open-world hints where they
apply. `server/discover`, `subscriptions/listen`, ping, cancellation, and
list-changed controls exercise current-version discovery and event behavior.

All control routes live under `/__mock/`: `/state`, `/reset`, `/reply-mode`,
`/list-changed`, and `/expire-session`. Set `bearerToken` in a local config to
require a synthetic bearer token; an absent or incorrect token gets 401 and a
`WWW-Authenticate: Bearer` challenge. Non-loopback Host values and mismatched
Origin values are rejected. No real provider or credential traffic is used.

## Run the fixture

From the repository root, with `lmmock`, `lmscript`, and `node` available:

```sh
node scripts/capture-fixture.mjs examples/mcp/server.lms build/mcp/server.lmcode
build/lmmock exec --lmcode build/mcp/server.lmcode \
  --config examples/mcp/streamable-http/config.json -- \
  node tests/mcp/client.mjs modern
```

The MCP client in `tests/mcp/client.mjs` is a small hand-written protocol
exercise. Run all transport/schema checks and 15 intentional handler mutations
with:

```sh
npm ci --prefix tests/mcp --ignore-scripts
node tests/mcp/run.mjs build/lmmock
```

`lmconn mcp` currently speaks MCP over stdio only; it has no HTTP MCP client.
Its generic HTTP command can make a direct current `server/discover` call
against the fixture without authentication. From a checkout of lmconn, run:

```sh
LMCONN_ROOT=/path/to/lmconn \
  build/lmmock exec --lmcode build/mcp/server.lmcode \
  --config examples/mcp/streamable-http/config.json -- \
  node tests/mcp/lmconn-client.mjs
```

That check uses only synthetic request metadata and loopback HTTP.

The vendored JSON schemas come from
`modelcontextprotocol/modelcontextprotocol` commit
`cf59bb0d8b43e711420f5f7313aa6cca3f1e1898`. The corresponding upstream license
and hashes are recorded in `tests/mcp/schema/`.

For consumers, pin an explicit released `lmmock` version in integration
tooling. Do not use a floating `@latest` or rolling latest release pin; the
mock's version advances on main pushes.
