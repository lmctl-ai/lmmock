# lmmock

Scriptable HTTP mock servers for integration tests. Go owns the loopback listener;
[lmcode](https://github.com/lmctl-ai/lmcode-go) implements the request handlers.
Use scripts to model your API, retries, malformed responses, latency, dropped
connections, and stateful workflows.

## Run a mock around your tests

Download the executable for your platform from
[Releases](https://github.com/lmctl-ai/lmmock/releases), or build it with Go 1.26:

```sh
go install github.com/lmctl-ai/lmmock/cmd/lmmock@latest
npm install -g @lmctl-ai/lmctl
lmmock exec --script examples/api.lms -- node examples/client.mjs
```

Run these examples from a checkout, or extract `examples.tar.gz` from a release.
The npm package installs both `lmscript` and `lmctl`. Source mode uses `lmscript`
(or `LMSCRIPT_BIN`) to compile the script before opening the listener.

`exec` binds `127.0.0.1:0`, starts the client command with `LMMOCK_URL` set to the
allocated URL, forwards its input/output, returns its exit code, and closes the
server when it finishes. Pass your own client test command after `--`:

```sh
lmmock exec --script examples/faults.lms -- go test ./integration/...
lmmock exec --script examples/stateful.lms -- npm test
```

The executable can run precompiled bytecode without Node or lmscript installed:

```sh
mkdir -p build
lmscript examples/api.lms build/api.dslb
lmmock exec --dsl build/api.dslb -- node examples/client.mjs
```

## Write handlers in lmcode

Export the exact host schema next to your script:

```sh
lmmock schema > schema.json
```

Create `mock.lms`:

```text
program = _lmcode_ schema="schema.json";
@health
fn healthcheck(x: Exchange) -> void implements Handler {
  x.json("{\"ok\":true}");
}
@echo
fn echo_body(x: Exchange) -> void implements Handler {
  x.status(201);
  x.send(x.body());
}
route("GET", "/health", "health");
route("POST", "/echo", "echo");
end _lmcode_
savedsl(program, ARGV[0]);
```

Annotations are identifiers. `route(method, path, annotation)` binds the endpoint
to an annotated function implementing `Handler(Exchange) -> void`. The top-level
body runs once to register routes; only the selected entry runs for each request.
Route registration during a request is an error. Missing or duplicate annotation
bindings, conflicting routes, incorrect interfaces, and invalid bytecode fail
before the server opens a port.

Paths use Go `net/http` ServeMux patterns, including `/users/{id}` and
`/files/{rest...}`. A trailing slash matches a subtree; `GET` also handles `HEAD`.
Go's path cleaning, redirects, 404 and 405 handling apply. Methods supported by
`route` are GET, POST, PUT, PATCH, DELETE, HEAD, and OPTIONS.

| Exchange method | Behavior |
| --- | --- |
| `method()`, `path()` | Request method and decoded URL path |
| `query(name)`, `param(name)`, `header(name)` | Query value, route parameter, or header; missing values are empty strings |
| `body()` | Raw request body bytes as an lmcode string |
| `status(code)` | Final status, 200–599; default 200 |
| `set_header(name, value)` | Set a response header |
| `send(body)` | Replace the response body, preserving bytes |
| `json(body)` | Set body and application/json content type; deliberately allows malformed JSON |
| `delay(milliseconds)` | Delay the response, 0–60000 ms; client cancellation stops the wait |
| `disconnect()` | Close the connection without sending an HTTP response |
| `get(key)`, `set(key, value)` | Shared string state for this server instance |
| `increment(key)` | Atomically increment a separate integer counter, starting at 1 |

Responses are buffered until the handler finishes. A VM error produces HTTP 500.
Repeated status/body/delay calls replace the previous value; headers and state are
updated as called. State persists across requests and resets on server restart.
Individual state operations are synchronized; a sequence of get/set operations
is not a transaction. Each concurrent request uses its own VM registry and arena.

## Included servers

| Script | Endpoints and purpose |
| --- | --- |
| [api.lms](examples/api.lms) | `/health`, binary `/echo`, and `/users/{id}` with bearer authentication and query parameters |
| [faults.lms](examples/faults.lms) | `/retry` fails twice with 503 then succeeds; `/slow`, `/disconnect`, `/malformed`, and `/error` simulate client failures |
| [stateful.lms](examples/stateful.lms) | POST `/webhook`, GET `/last-event`, and atomic `/count` |

Copy and modify these scripts for your tests. The standard lmcode string, math,
JSON, collection, codec, and file functions are available as well.

## Protocol mock servers

- `mongodb`: auth-less in-memory MongoDB wire mock; ping, insert/find/delete, numeric `$gt`, basic `$set` updates/count and database/collection listing. Config: `{"maxDocuments":10000}`; SCRAM deferred.

Beyond scripted HTTP, `lmmock proto` serves binary/text wire protocols from
built-in packages under `internal/proto/`:

```sh
lmmock proto --list                                  # registered protocols
lmmock proto redis --ready-file ready.json           # canned Redis (RESP) server
lmmock proto redis --listen 127.0.0.1:0 --config cfg.json
```

Readiness JSON carries `url`, `address`, `protocol`, and `pid`; the lifecycle
(loopback-only listeners, atomic ready file, SIGINT/SIGTERM shutdown) matches
`serve`. Each protocol package self-registers from `init` and is blank-imported
in `cmd/lmmock/proto_imports.go`; its `--config` schema is documented in the
package doc comment.

- `redis`: in-memory RESP2 strings, TTL, lists, DB selection, pub/sub and simulated READONLY; config `{"requirepass":"dummy","readonly":false}`.
- `mqtt`: in-memory Mochi broker with QoS 0/1/2 and optional auth; config `{"users":{"u":"p"}}` (default allow-all). Readiness uses `mqtt://`; lmclient expects the same address with `tcp://`.
- `smtp`: SMTP sink with AUTH PLAIN/LOGIN; config `users` and optional `spool` JSONL file. [Schema and limits](internal/proto/smtp/smtp.go).
- `imap`: seeded INBOX or live SMTP spool; LOGIN, LIST/STATUS/SELECT, FETCH/SEARCH and UID variants. Config `users`, `spool`, `messages`; no TLS. [Schema and limits](internal/proto/imap/imap.go).

## Standalone lifecycle

```sh
lmmock serve --script examples/api.lms --ready-file ready.json
```

`serve` prints one readiness JSON line, for example
`{"url":"http://127.0.0.1:54321","pid":1234}`, after the listener is open. It can
also atomically write the same object to `--ready-file`; the file's directory must
exist. Graceful shutdown removes that file. Use `--listen 127.0.0.1:8080` or
`--listen [::1]:0` to choose another loopback address. Non-loopback addresses and
hostnames are rejected. SIGINT/SIGTERM initiate shutdown; in-flight requests have
up to two seconds before connections are forcibly closed. Prefer `exec` for
portable test cleanup, including Windows.

Scripts are trusted test code, with the same filesystem privileges as the server.
They are not sandboxed. Request bodies are limited to 1 MiB, header reads to five
seconds, and request reads to ten seconds. There is no VM instruction timeout:
a script with an infinite loop must be terminated with its server process. These
mocks implement HTTP/1.x, not TLS, WebSockets, streaming responses, or arbitrary
wire protocols. `exec` manages its direct child; the child should clean up any
background descendants it creates. Abrupt process termination may leave a ready
file behind, so use a fresh path for each test run.

## GitHub Actions

After installing `lmmock` and `npm install -g @lmctl-ai/lmctl`, wrap the integration
command in one step; no background shell process or fixed port is needed:

```yaml
- name: Test against mock API
  run: lmmock exec --script test/mocks/api.lms -- npm run test:integration
```

This repository's workflow builds and tests Linux x64/arm64, macOS x64/arm64 and
Windows x64. Every successful main push publishes the tested native binaries,
example scripts, licenses and SHA256SUMS. Pull requests validate without publishing.
All binaries use CGO_ENABLED=0. Linux x64 also runs the Go race detector.

## Develop

Install Go 1.26 and Node 24, then:

```sh
npm install -g @lmctl-ai/lmctl
lmscript test.lms
# Equivalent: lmctl test.lms or ./test.sh
```

The lmscript driver compiles examples and negative fixtures, runs Go tests,
builds the executable, then invokes real-process HTTP integration tests. Those
cover concurrency, schema/interface/route rejection, request limits, binary bodies,
all fault scenarios, source compilation, child exit propagation and shutdown.
`go test ./...` requires the fixtures generated by `test.lms` first.

MIT licensed. See [LICENSE](LICENSE) and [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).

## OAuth2 and connector fixtures

[examples/oauth.lms](examples/oauth.lms) implements authorization-code and refresh
flows, exact redirect matching, HTTP Basic/body client authentication, S256 PKCE,
atomic one-use codes, code expiry, refresh rotation, and bearer userinfo. Policy
is in lmcode; the host supplies general encoding, hashing, random-token and
atomic-state primitives. These endpoints model a test identity provider, not a
production authorization service.

Run with `--config oauth-config.json`:

```json
{"clients":{"test":{"clientSecret":"fixture-secret","redirectUris":["http://127.0.0.1:5558/callback"],"authStyle":"header"}},"requirePkce":true,"rotateRefreshTokens":true}
```

Configuration is read on each request. `authStyle` accepts `header`, `body`, or
`both`. Options include `expiresIn`, `codeLifetimeMs`, `allowPlainPkce`,
`formEncodedTokenResponse`, and `misbehave`: `bad-state`, `bad-json`,
`string-expires`, `missing-expiry`, `extra-fields`, `server-error`, `rate-limit`,
`timeout`, or `disconnect`. Optional `logFile` receives redacted audit events;
create it before starting. Use synthetic credentials and keep configuration
files private. The lmconnectors integration tests use these handlers against
real loopback sockets; JavaScript handles process lifetime and assertions.

New host primitives are `config()`, `form_get(body,key)`, `url_encode/decode`,
`basic_id/secret`, `random_token()`, `sha256_url(text)`, `now_ms()`,
`secure_equal(a,b)` and `json_quote(text)`. `Exchange.take(key)` atomically reads
and deletes shared state. `Exchange.request_json()` provides method, URL,
headers and body for test assertions. The connector/LLM fixtures expose this
through `/__inspect`; that endpoint contains request data and must only receive
synthetic test credentials. It is not a request log.

## LLM and agent workflow tests

[examples/llm.lms](examples/llm.lms) serves the shared OpenAI-compatible route family below. It returns
OpenAI-compatible text, usage, tool calls and SSE-formatted responses. When tools
are supplied it requests `lookup` (or the first supplied function) with Paris as
the city. Submit a `role: tool` message to receive the final answer. This is a
deterministic protocol fixture, not a language model. SSE events are buffered in
one response; this tests event parsing, not incremental network timing.

Configuration `mode` selects `ok`, `echo`, `unauthorized`, `rate-limit`,
`server-error`, `bad-json`, `timeout`, `disconnect`, `retry`, or `unknown-model`.
`retry` returns 429 once per server instance, then succeeds; 429 replies include
`Retry-After: 1` (lmclient currently waits a fixed three seconds). Optional
`apiKey` requires an exact matching bearer header; missing or invalid keys get
401. `examples/llm-config.json` uses the synthetic key `fixture-key`.

| lmclient check / provider | Canned POST route | Shape |
| --- | --- | --- |
| `llm chat deepseek`, API probe | `/chat/completions` | OpenAI-compatible, bearer auth |
| `llm chat openrouter`, API probe | `/api/v1/chat/completions` | Same shape and auth |
| `llm chat ollama-cloud`, API probe | `/v1/chat/completions` | Same shape and auth |
| Existing offline DeepSeek override, tool round trip, SSE parser | `/v1/chat/completions` | Same handler; optional tools / stream |

Set `LMCLIENT_LLM_DEEPSEEK_BASE_URL=$LMMOCK_URL`,
`LMCLIENT_LLM_OPENROUTER_BASE_URL=$LMMOCK_URL/api/v1`, and
`LMCLIENT_LLM_OLLAMA_CLOUD_BASE_URL=$LMMOCK_URL/v1` for offline API calls.
Each accepts `model`, `max_tokens`, and system/user `messages`, returning `pong`,
the requested model, and integer usage (`2` prompt, `2` completion, `4` total).
Reserved models `overloaded` and `unknown-model` return 429 and a canned 404
`error.code: model_not_found`, respectively. The references do not specify a
provider-specific unknown-model payload; this fixture uses the common OpenAI
error envelope rather than claiming exact vendor error parity.

For compatibility with lmconn redaction tests, explicit modes `ok`, `echo`, and
`timeout` retain `served-model`; `responseModel` overrides the response model
(e.g. `fixture-model` for migration of lmclient's Python fixture). `echo` returns
the bearer value so clients can check redaction. Use no mode to echo the requested
model. The native suite checks all three paths, auth failures, errors, tools and
buffered SSE; protocol tests also check retry recovery, headers and request fields.

Both current provider registries implement `claude`, `codex`, `agy`, and `kimi`
as subprocesses, not HTTP APIs. There is no Anthropic `/messages` or Kimi HTTP
adapter to mock, and lmclient does not request streaming. CLI fixtures and
client-side missing-key/argument validation stay in lmclient's suite. Migrating
that Python suite's server lifecycle and assertions is separate work. See [llm-client.json](examples/llm-client.json) for a
complete tool-call round trip.

lmmock includes its own native assertion client:

```sh
lmmock exec --script examples/llm.lms --config examples/llm-config.json -- lmmock check --suite examples/llm-client.json
# For a server started with the same llm-config.json:
lmmock check --url http://127.0.0.1:8080 --suite examples/llm-client.json
```

A suite has a nonempty `steps` array. Each step supports `name`, `method`, `path`,
`headers`, `body` (JSON or raw string), expected `status` (default 200), `json`
(dot-path assertions, including numeric array indexes), `contains`, `capture`
(variable name to JSON path), `timeoutMs` (default 5000) and `networkError`.
Captured values are inserted with `${name}` in later requests and assertions.
Redirects are not followed. Responses are limited to 4 MiB. Output is one JSON
summary with a pass count and exit 0/1; response bodies and captured tokens are
never printed. Invalid CLI usage exits 2.

[examples/lmcurl-client.lms](examples/lmcurl-client.lms) independently checks chat
and SSE using released lmcurl v0.1.2. That release reads bytecode v5, so its test
uses an isolated `@lmctl-ai/lmctl@0.2.426` compiler. Compile the lmmock server with
the current compiler first; do not replace it with the v5 compiler. Linux CI
runs this compatibility check against downloaded, checksum-verified lmcurl
binaries. macOS/Windows run the native lmmock client and protocol suites.
