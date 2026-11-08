# Scriptable MCP HTTP mock

`examples/mcp/server.lms` exposes loopback-only protocol fixtures that run inside
`lmmock exec`. The transport revisions are deliberately separated:

- `examples/mcp/streamable-http/config.json` defaults to the current, stateless
  Streamable HTTP version, 2026-07-28. It accepts `POST /mcp`, requires
  `MCP-Protocol-Version`, `Mcp-Method`, the matching version in request `_meta`,
  `Mcp-Name` (including `=?base64?...?=` RFC 2047-style encoding for non-ASCII
  values) on `tools/call`, `resources/read`, and `prompts/get`, and both media
  types in `Accept`. JSON and request-scoped SSE replies can be selected with
  `X-Mock-Reply-Mode: json|sse` or `POST /__mock/reply-mode`.
- `tests/mcp/session-config.json` selects the older Streamable HTTP revision
  2025-11-25 for session tests. It supports initialize/initialized version
  negotiation, `Mcp-Session-Id`, GET event streams (flushing HTTP 200 headers
  immediately upon open), DELETE, expiry, 400 for missing `Mcp-Session-Id` on
  post-initialize requests, 404 for unknown or expired sessions, and
  `notifications/cancelled` (which terminates an in-flight `slow` request with
  204 No Content without emitting a JSON-RPC result). Its GET stream retains the
  latest mock list-changed event, delivers events to all concurrent session
  listeners without dropping notifications across streams, and replays missed
  events when `Last-Event-ID` is older.
- `examples/mcp/http-sse/config.json` and the same script provide the deprecated
  2024-11-05 HTTP+SSE shape: `GET /sse`, its `endpoint` event, then
  `POST /messages?session_id=...`; JSON-RPC replies, `slow` progress
  notifications (`notifications/progress`), and `notifications/tools/list_changed`
  are sent as `message` events on `/sse`.

The current mock offers `echo`, numeric `add`, cooperative `slow`, `fail`,
`crash`, and `delete_everything`. The last tool only increments a local call
record. Resources include text (`mock://text`) and base64 blob (`mock://blob`)
examples plus a resource template (`mock://item/{id}`); the `greet` prompt takes
a `name`. `tools/list` advertises JSON input/output schemas and read-only,
destructive, idempotent, and open-world hints in 2025-11-25 and 2026-07-28, and
2024-11-05-compliant tool definitions (`name`, `description`, `inputSchema`) on
the legacy HTTP+SSE transport. `server/discover` and `subscriptions/listen`
exercise 2026-07-28 discovery and subscription event behavior.

All control routes live under `/__mock/`: `/state`, `/reset`, `/reply-mode`,
`/list-changed`, and `/expire-session`. Control routes enforce loopback `Host`
validation, refuse any `Origin` header, and honor optional `bearerToken`
authentication. On `/mcp`, `/sse`, and `/messages`, non-loopback `Host` values
and cross-origin `Origin` values (matching `http://<Host>` or `https://<Host>`
under `--tls`) are rejected with 403, and an absent or incorrect bearer token
gets 401 with a `WWW-Authenticate: Bearer` challenge. No real provider or
credential traffic is used.

## Behavior changes since v0.1.110

Downstream consumers (such as `lmmcp` remote-MCP integration tests in `wp354`
and `lmconn`) should note the following wire-level fixes introduced after
`v0.1.110` and repin to the latest release tag if their tests rely on updated
protocol behavior:

1. **Immediate HTTP 200 header flush on long-lived SSE streams (`F1`)**:
   - `2025-11-25` `GET /mcp` now commits and flushes `200 OK` headers
     immediately upon stream open instead of waiting until the first SSE event.
   - `2026-07-28` `subscriptions/listen` now immediately flushes `200 OK`
     headers with `notifications/subscriptions/acknowledged` (carrying
     `_meta["io.modelcontextprotocol/subscriptionId"]` and `notifications`),
     attaches `_meta["io.modelcontextprotocol/subscriptionId"]` to emitted
     notifications, and closes the stream with a valid `SubscriptionsListenResult`
     (`resultType: "complete"`).
2. **Guard enforcement across all routes (`F2`)**:
   - `GET /mcp` and `DELETE /mcp` now enforce loopback `Host`, same-origin
     `Origin`, and optional `bearerToken` checks.
   - `/__mock/*` control routes now refuse any non-loopback `Host`, refuse any
     non-empty `Origin` header (`403`), and enforce `bearerToken` (`401`) when
     configured.
3. **Strict per-version schema compliance (`F3`)**:
   - `server/discover` now includes
     `_meta["io.modelcontextprotocol/serverInfo"]` (`{"name":"lmmock-mcp","version":"1.0.0"}`)
     and omits unimplemented `resources.subscribe` and `logging` capabilities.
   - `server/discover` and `subscriptions/listen` are only served in
     `2026-07-28` and return `-32601` in `2025-11-25` and `2024-11-05` so
     auto-negotiating clients (such as the official `go-sdk`) fall back cleanly
     to `initialize`.
   - `ping` and `initialize` return HTTP `404` with `-32601` in `2026-07-28`
     (`ping` remains supported with HTTP `200` and `{}` in `2025-11-25` and
     `2024-11-05`).
   - `2026-07-28`-only fields (`resultType`, `cacheScope`, `ttlMs`) are emitted
     only in `2026-07-28` (`cacheScope` and `ttlMs` only on cacheable list/read/discover
     results), and `structuredContent` is emitted only on `echo` and `add` in
     `2025-11-25` and `2026-07-28`.
4. **Cancellation semantics per transport revision (`F4`)**:
   - `2025-11-25`: `notifications/cancelled` aborts an in-flight `slow` request,
     increments `cancelledSlow`, and completes the cancelled JSON request with
     HTTP `204` and an empty body (no JSON-RPC response is sent for a cancelled
     request). Closing an HTTP connection in `2025-11-25` without sending
     `notifications/cancelled` does not increment `cancelledSlow`.
   - `2024-11-05`: `POST /messages` handles `notifications/cancelled` by
     cancelling the matching in-flight `slow` request and suppressing its final
     JSON-RPC response; `slow` also emits `notifications/progress` onto `/sse`.
   - `2026-07-28`: `POST /mcp` accepts `notifications/initialized` and
     `notifications/cancelled` with `202 Accepted`.
5. **HTTP status codes and header/resource/prompt validation (`F5`, `F6`)**:
   - `2026-07-28`: unknown methods return HTTP `404` with `-32601`; missing or
     `_meta`-mismatched `MCP-Protocol-Version` returns HTTP `400` with `-32020`
     (`HeaderMismatch`); `Mcp-Name` supports `=?base64?...?=` decoding; client-sent
     JSON-RPC responses on `POST /mcp` are rejected with HTTP `400` (`-32600`).
   - `2025-11-25`: missing `Mcp-Session-Id` on post-initialize requests returns
     HTTP `400` (`404` is reserved for unknown or expired session IDs); `Accept`
     is validated on `POST /mcp` and `GET /mcp`; `initialize` negotiates by
     returning the server's `protocolVersion` when the client offers a different
     non-empty version.
   - `resources/read` serves `mock://text`, `mock://blob`, and
     `mock://item/{id}`, and returns `-32602` (`2026-07-28`) or `-32002`
     (`2025-11-25` / `2024-11-05`) on unknown URIs. `prompts/get` returns
     `-32602` on unknown prompt names.
   - Malformed JSON (`400` `-32700`), JSON-RPC batch arrays or non-objects
     (`400` `-32600`), requests missing `id` (`400` `-32600`), and non-numeric
     `add` arguments (`-32602`) return structured errors instead of HTTP `500`.

## Run the fixture

From the repository root, with `lmmock`, `lmscript`, `go`, and `node` available:

```sh
node scripts/capture-fixture.mjs examples/mcp/server.lms build/mcp/server.lmcode
build/lmmock exec --lmcode build/mcp/server.lmcode \
  --config examples/mcp/streamable-http/config.json -- \
  node tests/mcp/client.mjs modern
```

Run all 7 transport/schema scenarios in `tests/mcp/client.mjs`, the official
`github.com/modelcontextprotocol/go-sdk` interoperability and `jsonschema-go`
wire validation suite in `tests/mcp/gosdk`, and all 28 intentional handler
mutations in `tests/mcp/mutations.mjs` with:

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
