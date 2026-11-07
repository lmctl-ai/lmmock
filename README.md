# lmmock

Scriptable HTTP mock servers for integration tests. Go owns the loopback listener;
[lmcode](https://github.com/lmctl-ai/lmcode-go) implements the request handlers.
Use scripts to model your API, retries, malformed responses, latency, dropped
connections, and stateful workflows.

## Run a mock around your tests

Download the executable for your platform from
[Releases](https://github.com/lmctl-ai/lmmock/releases), or build it with Go 1.26:

```sh
lmscript examples/api.lms node examples/client.mjs
```

Run these examples from a checkout, or extract `examples.tar.gz` from a release.
The npm package @lmctl-ai/lmctl supplies lmscript (0.2.454+). A source program
constructs an opaque `_lmcode_` value, then calls `run_lmcode`. The interpreter writes
and removes the private program file; there is no public compile/export step.

```sh
lmscript examples/faults.lms go test ./integration/...
lmscript examples/stateful.lms npm test
```

The source's command template is `lmmock exec --lmcode $DSLFILE -- CLIENT...`.
`exec` binds loopback, gives the client LMMOCK_URL, and closes the listener when it
exits. Under run_lmcode, client stdout is returned as result.stdout (with result.code = 0), and stdout ends in exactly
one SDK invocation frame. Host load/run failures exit 3; client failures are
application failures (exit 4). Standalone exec retains the client's exit status.
Dry invocation validates bytecode but starts no listener/client and creates no audit log.
The low-level `--lmcode FILE` input is owned by lmscript, not a filename returned to scripts.

Build/test tooling uses `node scripts/capture-fixture.mjs SOURCE.lms OUTPUT.lmcode`
to capture the private host input for embedded presets and byte-level mutation tests.
This is a trusted fixture adapter, not a production compiler API. Non-HTTP handler
sources use caller-supplied invocation command argv; their native stdio/wire protocol
engines consume embedded/captured fixtures rather than the HTTP exec command.

## Built-in LLM API mock (no Node, no lmscript)

The release binary embeds the compiled [examples/llm-api.lms](examples/llm-api.lms)
(OpenAI chat/responses, Anthropic `/v1/messages`, Gemini, Ollama, `/__inspect`,
`/__requests`). Use it without any compiler:

```sh
lmmock exec --builtin llm-api -- your-llm-client-test
lmmock serve --builtin llm-api --config cfg.json --audit-log requests.jsonl
```

`--builtin` is mutually exclusive with `--lmcode`. Every reply is
`pong` unless `--config` sets `"text"`, or `"replies"` scripts per-turn answers
keyed on the request body (the rule found latest in the body wins). Coding CLIs always send `tools`; the mock
normally answers such a request with a tool call, so pass `{"toolCalls": false}` in
the config to always reply with plain text. See [docs/llm-api.md](docs/llm-api.md).
Regenerate the embedded bytecode after editing the script with `scripts/compile-builtin.sh`.

`--builtin oauth-codex-device` mocks the OpenAI auth service for `codex login --device-auth`
(device code, PKCE exchange, API-key exchange, refresh with rotation); see
[docs/oauth-codex-device.md](docs/oauth-codex-device.md) and `scripts/codex-login-e2e.sh`.

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
r = run_lmcode(program, {command: concat(["lmmock", "exec", "--lmcode", "$DSLFILE", "--"], ARGV)}); require(r.ok, "mock client failed");
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
| `disconnect()` | Close the connection; after a committed stream its prefix remains visible |
| `stream_event(bytes)` | Write and flush ordered bytes immediately during the Handler call |
| `stream_fault(kind)` | HTTP/1 disconnect, truncated-chunk or malformed-chunk after an optional prefix |
| `request_log()` | Redacted request-history JSON, enabled with --request-log |
| `reset()` | Clear shared strings, counters and request history; used by lmcode admin routes |
| `get(key)`, `set(key, value)` | Shared string state for this server instance |
| `increment(key)` | Atomically increment a separate integer counter, starting at 1 |

send/json responses remain buffered until the handler finishes. stream_event commits and flushes
during the handler call. A VM error before commit produces HTTP 500; after commit
it aborts the stream rather than appending another response.
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
| [jira-xray.lms](examples/jira-xray.lms) | Stateful Jira/Confluence MCP, OAuth stubs and Xray GraphQL; [contracts and setup](docs/jira-xray.md) |

## Authentication-flow examples

The mock-only auth examples cover GitHub OAuth device authorization and REST token
inspection, OAuth 2 authorization-code + PKCE (plus a client-credentials token grant),
and npm registry web login/profile/token inspection. They use only fixture clients and
synthetic credentials. `/__mock/*` routes are local approval/clock controls, not public
provider endpoints. See [docs/auth-mocks.md](docs/auth-mocks.md) and the individual
example READMEs under [examples/auth](examples/auth).

The auth protocol checks run in the normal `lmscript test.lms` gate, including deliberate
mutants for GitHub `slow_down`, PKCE verifier validation and npm's 202 pending response.

Copy and modify these scripts for your tests. The standard lmcode string, math,
JSON, collection, codec, and file functions are available as well.

## Protocol mock servers

- `kafka`: one in-memory plaintext broker (kfake); optional `{"topics":{"events":1}}` seeds partitions, missing topics auto-create; supports lmclient ping/produce/consume, including idempotent producers. See [config and Java verification](docs/kafka.md).

- `mongodb`: auth-less in-memory MongoDB wire mock; ping, insert/find/delete, numeric `$gt`, basic `$set` updates/count and database/collection listing. Config: `{"maxDocuments":10000}`; SCRAM deferred.
- `postgres`: PostgreSQL v3 startup/auth, simple and prepared queries, canned SQL fixtures (`lmmock proto postgres`).

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

- `amqp`: memory-only AMQP 0-9-1 queues, publisher confirms and auto-ack `basic.get`; config `{"users":{"u":"p"}}` (default accepts any PLAIN credentials). See [AMQP mock notes](docs/protocol-amqp.md).

## Standalone lifecycle

### Selective upstream fallback

Keep explicit `route()` mocks and forward only paths with no registered route to one fixed
upstream with the `serve`/`exec` options below. No lmcode or schema change is needed,
so existing precompiled scripts remain compatible:

```sh
lmmock serve --lmcode "$PRIVATE_PROGRAM_FILE" --upstream http://127.0.0.1:8080/repository/public/ \
  --upstream-timeout 2m --upstream-read-only --audit-log requests.jsonl
```

Normal route matching wins, including GET handling HEAD; other methods on a mocked
path retain the mux's 405 and Allow header. A handler's own 404 never falls through.
Avoid a catch-all `route("GET", "/", ...)` when you want GET
passthrough. With no `--upstream`, existing 404/405 behavior is unchanged. The
upstream URL must be HTTP(S), without user credentials, query or fragment. Its path
prefix is prepended to the original escaped request path; the original query,
method, end-to-end headers and body are preserved. Host changes to the upstream
authority. Dot segments (`.` or `..`, including URL-encoded forms) are rejected
with 400 before forwarding; empty path segments are preserved. A forwarded
asterisk request target is also rejected with 400. Connection, Keep-Alive, Proxy-*, TE, Trailer, Transfer-Encoding, Upgrade,
and headers named by Connection are removed in both directions. HTTP framing may
change; response status, end-to-end headers, Location and body bytes do not.

Passthrough streams request/response bodies outside the VM, including large
artifacts; send/json responses still buffer; stream_event responses flush incrementally. Go's normal TLS certificate validation
applies outbound; the inbound listener uses loopback HTTP or explicitly enabled HTTPS. The client does not
follow redirects, decompress bodies, use environment proxies, inject credentials,
or add forwarding headers. The upstream transport uses HTTP/1.1 only, including
over TLS. Caller-supplied Authorization/Cookie headers are normally preserved.
Optional `--upstream-read-only` strips Authorization, Proxy-Authorization and Cookie
and rejects unmatched methods other than GET/HEAD with 405; explicit mocks are unaffected.

Defaults are a two-minute total request deadline, five-second connect/TLS limits,
and a fifteen-second response-header limit (bounded by the total deadline).
The total deadline is not an idle timeout: raise it for large artifacts on slow links.
Inbound forwarded uploads must still complete within the server's ten-second
request-read timeout; read-only forwarding does not support uploads.
Unreachable upstreams return 502; timeouts before response headers return 504,
with a clear error body. A timeout/error after response headers aborts the response
connection: its already-committed status cannot be replaced. This is a fixed
fallback, not a CONNECT tunnel, retry engine or general proxy framework.

`--audit-log` appends JSONL records with `kind` (`mocked`, `forwarded`, `unmatched`),
method, original URI/Host/headers, response status/body-byte count, elapsed time,
and upstream/error where applicable. Authorization, Proxy-Authorization and Cookie
values are redacted; bodies are never logged. A partial response records its
committed status and error, not a completed transfer. Audit writes are serialized
within the server and are best effort. Client disconnects are audited as
`client closed request`. The CLI prints the audit-log path to stderr at startup.

```sh
lmmock serve --lmcode "$PRIVATE_PROGRAM_FILE" --ready-file ready.json
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
mocks implement HTTP/1.x with optional TLS and ordered streaming. WebSocket and
HTTP/2/gRPC adapters are separate protocol slices. `exec` manages its direct child; the child should clean up any
background descendants it creates. Abrupt process termination may leave a ready
file behind, so use a fresh path for each test run.

## GitHub Actions

After installing `lmmock` and `npm install -g @lmctl-ai/lmctl`, wrap the integration
command in one step; no background shell process or fixed port is needed:

```yaml
- name: Test against mock API
  run: lmscript test/mocks/api.lms npm run test:integration
```

This repository's workflow builds and tests Linux x64/arm64, macOS x64/arm64 and
Windows x64. Pushes to main (unless `[skip ci]` is in the commit message) and manual workflow dispatches publish the tested native binaries,
example scripts, licenses and SHA256SUMS as a rolling `v0.1.<run_number>` release. Downstream consumers should pin a specific release tag and verify `SHA256SUMS`. Pull requests validate without publishing.
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

## n8n connector presets

Canned fixtures for the HTTP services behind the most-used n8n connector nodes,
so an lmclient or lmscript connector for them can be tested offline on every
platform. Each fixture enforces the vendor's real auth shape, answers the core
routes its n8n node exercises with deterministic data, returns the vendor's own
error shape on missing or wrong credentials, and has a fault route for
retry/backoff testing. Defaults can be overridden with `--config` (see each
fixture's header comment); the dummy credentials are fixtures, never real keys.

| Fixture | Mined from (n8n `packages/nodes-base`) | Auth | Core routes | Fault route |
| --- | --- | --- | --- | --- |
| `examples/slack.lms` | `nodes/Slack/V2`, `credentials/SlackApi` | `Authorization: Bearer xoxb-fixture-token`; failures are HTTP 200 `{"ok":false,"error":"not_authed"\|"invalid_auth"}` | `/api/auth.test`, `chat.postMessage`, `chat.update`, `conversations.list`, `users.list`, `users.info` | channel `C0RATELIMIT` -> 429 `ratelimited` + Retry-After |
| `examples/discord.lms` | `nodes/Discord/v2/transport`, `credentials/DiscordBotApi`, `DiscordWebhookApi` | `Authorization: Bot fixture-bot-token`; 401 `{"message":"401: Unauthorized","code":0}`; webhooks by path token | `/api/v10/users/@me/guilds`, `guilds/{id}/channels`, `guilds/{id}/members`, `channels/{id}/messages` (GET/POST), `/api/webhooks/{id}/{token}` | channel `...429` -> 429 `retry_after`; `...500` -> 500 |
| `examples/telegram.lms` | `nodes/Telegram/GenericFunctions.ts`, `credentials/TelegramApi` | token in the path `/bot123456:fixture-telegram-token/<method>`; 401 `{"ok":false,"error_code":401}` | `getMe`, `sendMessage` (JSON or form), `sendPhoto`, `sendDocument`, `getChat`, `getUpdates`, `sendChatAction` | `chat_id` 429 -> 429 with `parameters.retry_after`; 500 -> 500 |
| `examples/jira.lms` | `nodes/Jira/GenericFunctions.ts` (`/rest/api/2`), `credentials/JiraSoftwareCloudApi` | HTTP Basic `fixture@example.test:fixture-api-token`; 401/404 `{"errorMessages":[...],"errors":{}}` | `/rest/api/2/myself`, `issue/{key}`, `POST issue` (201), `search`, `project` | issue `FIX-429` -> 429 + Retry-After; `FIX-500` -> 500 |
| `examples/stripe.lms` | `nodes/Stripe/helpers.ts` (form bodies), `credentials/StripeApi` | `Authorization: Bearer sk_test_fixture`; 401 `{"error":{"type":"invalid_request_error"}}`, key never echoed | `/v1/balance`, `customers` (GET/POST form, `{id}`), `charges` (GET/POST form), `tokens` | `amount=429` -> 429 `rate_limit_error`; `amount=500` -> 500 `api_error` |

Additional annotated lmcode presets cover **Notion, Airtable, HubSpot, GitLab,
Asana and Trello**. See [routes, authentication and limits](docs/http-presets.md).
They support stateful create/read operations, pagination and deterministic faults.

Each preset ships an assertion suite, `examples/<service>-client.json`, run by
`tests/protocols.mjs`:

```sh
lmmock exec --lmcode build/slack.lmcode -- lmmock check --suite examples/slack-client.json
```

## LLM and agent workflow tests

Provider-wire fixtures use the shared versioned contract in
[`fixtures/providers/README.md`](fixtures/providers/README.md). Each profile pins
the protocol source/version, model family, lmcode handler and declared routes,
then exercises success and deterministic adverse cases through the same runner.
The runner supports HTTP requests and shell-free fake-client processes; it records
expected model/usage/tool/stream behavior and optional lmctl `error_class`. Fault
profiles contain no sleeps or test timeout knobs. `test.lms` runs every manifest
under `fixtures/providers/` plus wrong-usage and changed-handler controls on all CI
platforms.

For a broader scriptable LLM API with **real Vercel AI SDK client
acceptance**, use [llm-api.lms](examples/llm-api.lms).
It supports OpenAI Chat/Responses, native Anthropic Messages and native Gemini
generateContent, plus OpenAI/Gemini embeddings, tools, structured output and SSE.
See [setup, tested clients and limits](docs/llm-api.md).

The native Ollama fixture also serves `/api/chat` and `/api/tags` without a Go
provider handler. Chat supports JSON replies, NDJSON streams, function tool calls
and tool-result history; the contract and deterministic error cases are in
[`fixtures/providers/ollama-ndjson.json`](fixtures/providers/ollama-ndjson.json).


[examples/llm.lms](examples/llm.lms) serves the shared OpenAI-compatible route family below. It returns
OpenAI-compatible text, usage, tool calls and SSE-formatted responses. When tools
are supplied it requests `lookup` (or the first supplied function) with Paris as
the city. Submit a `role: tool` message to receive the final answer. This is a
deterministic protocol fixture, not a language model. SSE records flush individually through an lmcode helper; no pacing timers or
recorded wall-clock delays are used.

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
ordered SSE; protocol tests also check retry recovery, headers and request fields.

Both current provider registries implement `claude`, `codex`, `agy`, and `kimi`
as subprocesses, not HTTP APIs. There is no Anthropic `/messages` or Kimi HTTP
adapter to mock, and lmclient does not request streaming. CLI fixtures and
client-side missing-key/argument validation stay in lmclient's suite. Migrating
that Python suite's server lifecycle and assertions is separate work. See [llm-client.json](examples/llm-client.json) for a
complete tool-call round trip.

lmmock includes its own native assertion client:

```sh
lmmock exec --lmcode "$PRIVATE_PROGRAM_FILE" --config examples/llm-config.json -- lmmock check --suite examples/llm-client.json
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

[examples/lmcurl-client.lms](examples/lmcurl-client.lms) checks chat and SSE with a
current lmcode-protocol lmcurl host. The retired v0.1.2/v5 binary cannot consume the
current protocol; its immutable-version compatibility gate is no longer run.

### GitHub Copilot CLI Integration

[examples/copilot.lms](examples/copilot.lms) provides a full mock fixture for `@github/copilot` CLI, supporting OpenAI completions (`/v1/chat/completions`), OpenAI responses (`/v1/responses`, `/responses`), Anthropic messages (`/v1/messages`), models catalog (`/models`), auto routing (`/auto`), platform telemetry, and tool execution round-trips:

```sh
# Run Copilot CLI against lmmock
lmmock exec --lmcode build/copilot.lmcode --config examples/copilot-config.json -- \
  node tests/copilot.mjs
```

## Capture a client protocol

Use lminspect as a reverse proxy when the client supports a base URL. The following
command assumes a protected working directory and an operator-chosen upstream;
raw recordings and tap output contain credentials. They are local inputs, never
CI artifacts or committed fixtures. For fixed URLs, the lminspect case study
covers a per-run CA, client trust, --ssl-bump and an explicit --route destination
scheme (HTTPS interception can forward to a plain HTTP mock).

```sh
lminspect run --listen 127.0.0.1:18080 --mode reverse:https://YOUR_UPSTREAM --record raw.jsonl
```

Point the client at that listener, use synthetic prompts, then stop only the
proxy process you started. No vendor capture runs in this repository's tests.

## Convert and scrub a capture

The Go converter handles lminspect JSONL, gzip bodies, raw binary data, HTTP error
responses and ordered SSE records. It gathers credentials across the whole capture
before scrubbing request/response bodies, paths and headers, including decoded JSON
string escapes. Authorization, API-key, token and cookie headers are replaced or
removed; response headers use an allowlist. Provider IDs and timestamps normalize
deterministically. --exact disables only dynamic normalization, never scrubbing.
Transport failures, incomplete flows, unsupported compression and secret residue
are errors; they are never silently omitted. Review scrubbed output for application
PII and unknown secret formats before sharing it: no finite pattern list identifies
all application secrets.

```sh
lmmock scenario scrub --input raw.jsonl --output flows.jsonl
lmmock scenario import --input flows.jsonl --output scenario.json
```

Files are written only after conversion passes. Raw credentials are not printed in
errors. Delete private raw captures and per-run CA material after review. Scrubbed
credential placeholders are safe fixture identities, not working credentials.

## Replay the Copilot capture preset

The capture-derived preset is captures/copilot/scenario.json. Its provenance names
the lminspect case study for CLI v1.0.105 and the local lmcode stand-in actually
recorded: this is not a claim of a newly recorded vendor session. Twelve exchanges
cover the required model flags/catalog, auto selection, platform handshake,
Responses API's nine sequential events, Anthropic messages, chat streaming and an
unauthorized response. The existing examples/copilot.lms remains the configurable
mock for clients whose prompts differ from this exact captured conversation.

Compile examples/scenario.lms with the matching examples/schema.json, then replay:

```sh
lmmock exec --lmcode build/scenario.lmcode --config captures/copilot/scenario.json -- YOUR_TEST_COMMAND
```

Handlers remain annotated lmcode entries implementing the Handler single-method
interface. The generic HTTP transport adds Exchange.stream_event(bytes): each event
is written and flushed in order during the Handler call, without delays, sleeps,
recorded timing or a timeout option. TCP packet boundaries are not event boundaries.
A handler error before emission is still an HTTP error. After stream commit, status,
headers, send/json and delay are refused; emitted bytes cannot be replaced. An error
then aborts the connection. Choose either event emission or a final buffered body.

## Add a preset and verify fidelity

Store reviewed scrubbed flows, the generated scenario and a provenance document
under captures/NAME. Keep raw files outside the checkout. The engine matches exact
sanitized method, URI/query, raw body and credential identity; it does not generalize
prompts or parse arbitrary request bodies as JSON. Identical requests consume their
recorded replies in occurrence order (for example 429 then 200). Unmatched or exhausted
rules return 501. POST /__reset resets the conversation; concurrent duplicate
requests receive occurrence numbers in arrival order. Scheme/host are intentionally
not matchers so a TLS capture can replay over loopback HTTP.

Run verification inside a fresh lmmock exec, where LMMOCK_URL identifies the server:

```sh
lmmock scenario verify --input captures/copilot/flows.jsonl --target "$LMMOCK_URL"
```

Verification permits only a loopback HTTP origin, disables redirects/environment
proxies, and compares normalized status, allowed headers, bytes and ordered SSE
records. It fails on changed replies, missing rules or reordered events. The normal
Go tests exercise these controls and per-event flushes. tests/scenario/controls.py
also removes the production credential scrub rule and requires a failing assertion.

The Capture fixture fidelity Actions job downloads a pinned npm compiler and a
checksummed lminspect release, records only the local stand-in, and compares its
regenerated scenario to the committed preset. It builds the candidate lmmock because
the new streaming primitive must be tested before release; it does not substitute an
older release binary. The normal test matrix also runs the fidelity suite. Pushes to main
and manual workflow dispatches publish a rolling release after all five platform builds and tests pass; downstream repositories should pin an explicit release tag and checksum. Native
Copilot CLI live integration is not part of the default offline gate.


## Incremental streams and framing faults

[examples/streaming.lms](examples/streaming.lms) is an annotated Handler preset.
`/tokens` emits two SSE records; `/chunks` emits two ordinary byte chunks.
`/fault/disconnect`, `/fault/truncated-chunk` and `/fault/malformed-chunk` emit a
valid prefix followed by the selected HTTP/1 framing fault. The truncation declares
a chunk longer than its remaining bytes; the malformed case writes a nonhex chunk
size. Clients must observe a partial body and a transport error, not a repaired
successful response. These raw chunk faults require HTTP/1.

stream_event writes immediately and flushes before the next lmcode instruction.
The normal HTTP server owns chunk framing; TCP packets are not event boundaries.
The scenario, Copilot, LLM and native-provider presets use this primitive without
changing their service payloads. No new sleep, latency, timeout or pacing feature
is provided. The older buffered delay fixtures are unchanged.

## TLS and per-run client trust

Enable HTTPS with `--tls`; repeat `--tls-host` to add fixed DNS/IP identities.
The listener still binds only a loopback IP. A fresh CA and server key are generated
for that server invocation. Only the public CA certificate is written to a private
temporary directory; private keys stay in memory. Close/shutdown removes the CA
file. Readiness JSON adds `ca_cert` and its URL uses https.

```sh
lmmock exec --tls --tls-host fixed.mock.test --lmcode "$PRIVATE_PROGRAM_FILE" -- YOUR_CLIENT
lmmock serve --tls --tls-host fixed.mock.test --lmcode "$PRIVATE_PROGRAM_FILE" --ready-file ready.json
```

The exec child receives LMMOCK_URL, LMMOCK_CA_CERT, SSL_CERT_FILE,
NODE_EXTRA_CA_CERTS, REQUESTS_CA_BUNDLE and CURL_CA_BUNDLE. These values replace
inherited values only inside that child; the parent and machine trust stores are
not changed. Node reads NODE_EXTRA_CA_CERTS when it starts. The native lmmock check
client explicitly loads LMMOCK_CA_CERT on Linux, macOS and Windows rather than
relying on platform-specific SSL_CERT_FILE behavior. Certificate verification
stays enabled; an untrusted client fails the TLS handshake.

A client with a fixed `https://fixed.mock.test/...` URL also needs local connection
routing while preserving the original URL authority/SNI. The acceptance client
uses an HTTPS Agent connection override to the allocated loopback port. curl can
use --connect-to together with --cacert. This server does not edit DNS/hosts files,
redirect traffic to a vendor, or implement a CONNECT proxy. For clients without a
trust-file option, a test-owned trust store is another choice: the distribution CA
store on Linux, a test keychain on macOS, or CurrentUser Root on Windows. The harness
must install/remove that public CA itself; lmmock never installs system trust.

## Request history and deterministic reset

Enable `--request-log` for redacted metadata; `--request-log-bodies` additionally
captures consumed body bytes and implies metadata logging. The scenario and streaming
presets expose GET /__requests and POST /__reset as ordinary annotated lmcode routes.
A new service can expose them by calling request_log/reset; no Go route is added.

History JSON has enabled and requests fields. Records are in arrival order with
id, method, uri, scrubbed headers, status, response bytes, complete and optional
error. Active requests remain visible with complete=false; faults keep their
committed status and error kind. Body logging is opt-in, limited to the first
1 MiB read by the handler/forwarder, with body_complete and body_truncated markers.
Unconsumed bodies are not invented. Encoded or binary bodies are omitted; use the
capture converter for byte-faithful recordings.

Credential headers, sensitive query/JSON-body keys, known credential echoes and the
converter's secret patterns are scrubbed. Unknown application secrets still need
review; these endpoints are for synthetic test traffic. Logging is off by default.
Admin calls do not log themselves. Reset clears strings, counters, occurrence
state and history together; an old in-flight log record cannot reappear afterward.
Reset is not rollback/cancellation of an already executing handler: quiesce concurrent
clients when starting a new deterministic scenario.

## Streaming and TLS acceptance

The normal test.lms and five-platform Actions matrix run the streaming/TLS client
and compiled production controls. A causal Go assertion sees each token flushed
before its handler advances; the old queued build fails that assertion. Node checks
separate SSE chunks at a fixed HTTPS URL, succeeds with the exported CA and fails
in a fresh untrusted process. Additional controls remove flush/CA export/reset/scrub
or repair malformed/truncated framing and require real assertion failures. No
wall-clock thresholds, sleeps or timeout mock behavior are used by these tests.
## Stdio ACP, MCP and agent fixtures

The stdio host uses newline JSON-RPC or Content-Length framing. Method behavior
lives in annotated lmcode functions implementing RPCHandler(RPC), including the
shipped ACP, MCP and MCP-client presets. Compiled presets are embedded in lmmock;
running them does not require a compiler or a source checkout.

```sh
lmmock stdio acp --scenario tests/stdio/scenario.json --alias Worker
lmmock stdio mcp --scenario tests/stdio/scenario.json --alias MCP
```

ACP supports initialize, the initialized notification, session/new, session/load,
mode/model/config-option calls and session/prompt. Prompt cells emit ordered
session/update text chunks followed by stopReason. Loading an unknown fixture
session fails. The stock preset does not simulate cancellation or native provider persistence; the ACP profile fixtures below add event-driven cancellation.
A ready-made multi-turn conversation script and the wire flow are in [docs/acp.md](docs/acp.md).
An unsupported method fails loudly instead of silently succeeding. Kimi/OpenCode
config-option and Hermes mode/model shapes are supported; vendor session files
and identity probes remain the host adapter's responsibility.

See [docs/mcp.md](docs/mcp.md) for a multi-call MCP session script.

MCP targets protocol 2025-11-25: initialize, tools/list, tools/call, resources/list,
resources/read, resources/templates/list, prompts/list, prompts/get and ping.
The default tool is fixture. A call emits a log notification and the cell's text;
unknown tools, resources and prompts fail. The default resource and prompt are
small explicit fixtures, not filesystem access. Paging, sampling, elicitation and
subscriptions are not claimed by these presets.

## Script turns and fake agent output

Scenario cells are exact (alias, turn) pairs. The alias is --alias or the cwd's
basename. Turns start at one; --state DIR preserves counters across one-shot
processes. Without it counters belong to one process. Duplicate/missing cells,
wrong expected prompt text and timing faults fail with nonzero exit. An error
cell emits the protocol error where applicable and stops; empty text must be
explicitly scripted. No wildcard/default reply is supplied.

```json
{
  "session": "fixture-session",
  "cells": [
    {"alias":"Worker","turn":1,"expect":"ping","text":"pong","chunks":["po","ng"]},
    {"alias":"Worker","turn":2,"error":"scripted failure"}
  ]
}
```

```sh
lmmock stdio agent --preset claude --scenario cells.json --alias Worker --state private-counters -- -p ping
lmmock stdio agent --preset codex --scenario cells.json --alias Worker
lmmock stdio agent --preset gemini --scenario cells.json --alias Worker -- -p ping
lmmock stdio agent --preset agy --scenario cells.json --alias Worker -- -p ping
```

Claude emits system/assistant/result stream-JSON; Gemini emits init/message/result;
agy emits plain print text, matching lmctl's non-streaming print transport. Codex
speaks initialize, thread/start or thread/resume and turn/start, then emits
turn/started, item/agentMessage/delta and turn/completed. One-shot prompts normally
arrive as argv; --stdin-prompt explicitly reads EOF-delimited prompt input for
Claude/Gemini/agy. No vendor CLI is invoked and no vendor session database is
fabricated. These are wire presets, not a claim of native seed/discovery parity.

State files contain counters only, keyed by alias and counter name. Concurrent
updates of the same counter fail closed instead of polling or sleeping; separate
aliases are independent. A crash may leave a lock requiring removal in the private
test directory. Keep one private directory per test run; do not share it with an
operator profile.

## Extend methods and tools with lmcode

Export the host declaration with lmmock stdio --schema. Compile a program against
those exact bytes and select it with --lmcode FILE. New method/tool policy needs
no Go edits. The top-level bind(method, annotation) registers a method; entries
are validated before input is read. MCP tools can be routed by lmcode through
x.forward("tool:" + name) to independent annotated handlers bound under that key.
Forwarding retains the original request ID and parameters and rejects cycles.

```text
@echo
fn echo(x: RPC) -> void implements RPCHandler {
  x.reply(x.params());
}
bind("example/echo", "echo");
```

RPC methods expose message(), method(), params(), alias(), get/set/next state,
reply(JSON), error(code,message), notify(method,JSON), request(method,JSON),
emit(JSON), write_text(bytes), forward(key), close() and abort(message).
request assigns a correlated ID; incoming responses dispatch to $response.
An optional $start entry can initiate a client exchange. Replies and notifications
flush without timing delays; stdout contains protocol data only. Diagnostics use
stderr. Missing replies, duplicate replies, unknown IDs and malformed frames fail.
Custom scripts are trusted, like other lmcode handlers.

## MCP client mock and owned peers

The MCP-client preset initializes a peer, lists the fixture tool, calls it, and
checks the text against scenario.expected. It answers ping and accepts log
notifications. The peer command must resolve inside --fake-bin after symlink
resolution; a real executable outside that directory is refused before spawn.
Use a private copy of the intended fixture binary in that directory.

```sh
lmmock stdio mcp-client --scenario cells.json --fake-bin private-bin -- private-bin/lmmock stdio mcp --scenario cells.json --alias MCP
```

For general protocol exercises, --framing content-length selects length-prefixed
frames; ACP/MCP defaults to newline framing. The client owns only its direct peer:
normal completion closes stdin and waits, and an error kills only that owned PID.
No deadlines, sleeps, retries or automatic real-binary fallback are provided.

## Published-client stdio verification

The standard test gate compiles the preset sources, runs framing/protocol/alias
checks, compiled mutation controls and an owned MCP client/server process test.
The Stdio published client integration Actions job downloads npm lmctl 0.2.448
privately and proves lmscript lm_agent and lm_prompt against the ACP preset through
a guarded miniplayer shim in a scratch HOME/database. A third unscripted turn must
remain a client-visible error. No model keys, production daemon or vendor traffic
are involved. The regular five-platform matrix runs the stdio unit/process gates.

The service-owned MCP topology remains distinct: lmbee owns connections; the mcp
CLI/schema functions are stateless clients. The tested npm package contains no
lmbee/mcp entrypoint, mcp-client is marked private in mono, and no public lmbee
release was available during this work. MCP-through-lmbee is therefore not claimed;
the standalone mock client/server proof does not substitute for that service gate.

### OpenAI HTTP fixture qualification

[OpenAI Chat and Responses fixtures](fixtures/openai/README.md) provide
route-specific synthetic auth, JSON and ordered event SSE, tool-call items,
usage and deterministic HTTP/parser faults. Normal native tests exercise the
SDK; the OpenAI HTTP published-client job also drives real npm lmctl through
pinned Miniplayer on loopback and checks session/token/error telemetry.

### Native Claude/Qwen JSONL processes

[The stream-json fixtures](fixtures/streamjson/README.md) use annotated lmcode
handlers with the generic JSONL process layer. Private copied executables in PATH
exercise published lmctl seed, prompt, native session readers and persistent batch
on all native platforms. Model families are manifest data. Missing result/error
classes and unavailable usage are pinned to observed client behavior; documented
client gaps are not disguised as successful rejection. No vendor traffic or real
credentials are involved.
## ACP provider-profile coverage

[ACP profile fixtures](fixtures/acp/README.md) extend the stdio host with one lmcode
handler and six P0 manifests. Copilot, Kimi, OpenCode, Hermes and Miniplayer run
through published lmctl adapters; Gemini's disabled adapter is asserted separately
from its wire profile. Model families stay in manifests. The matrix checks auth
error classes, session/configuration effects, tool updates, replay isolation,
unknown billing facts and event-driven cancellation on all five native platforms.

```sh
node tests/acp-profiles/run.mjs
```

Active cancellation uses generic deferred JSON-RPC replies; protocol policy stays
in lmcode. Tests do not use sleeps, provider credentials or vendor traffic. See the
profile notes for exact expected warnings, conservative error classes and the
Linux-only native SIGTERM qualification.
### Codex app-server provider fixtures

The [Codex fixture contract](fixtures/codex/README.md) adds initialize, thread
start/resume/fork, ordered turn/usage events, interruption and app-server failures
as annotated lmcode RPCHandler methods. Model families are manifest data. The
native process fixtures run the privately downloaded lmctl client and assert its
wire, session and accounting results on all five test platforms. The documented
strict controls expose published-client classification/accounting gaps; those
requirements are not claimed as covered merely because the mock can emit them.


## Print and sidecar provider process fixtures

[Lane E profiles](fixtures/providers/lane-e/README.md) exercise published lmctl
against owned native fake executables for Agy/Pi print, lmplayer run/JSONL, DSH
sidecar HTTP RPC and Muse MSP. Models are fixture-family data. Annotated lmcode
handlers own output/events/failures; Go supplies generic process/listener and
compressed-record framing. The suite checks native stdout/stderr/exit, real
adapter error classes, model argv, native session resume and token facts.

The shared P0 manifests are under fixtures/providers/lane-e/manifests. Normal
five-platform tests and a dedicated published-client workflow run them and compiled
handler mutations. Known zero-exit empty-output defects in published lmctl 0.2.448
are reported explicitly as unqualified client gaps, not successful cases. The lane
README documents the supported process contracts and the unclaimed native/TUI APIs.
## WebSocket, HTTP/2 and gRPC handlers

The `wire` family adds `ws`, `http2`, `grpc` and a worked `line` adapter. Go supplies
protocol framing; annotated lmcode SAM entries implement applications. A new gRPC
method needs its supplied descriptor and handler, not generated Go application code.
The runtime remains one CGO-disabled executable; Node is used only for source fixture
compilation with the repository's published compiler pin.

### Scenario and lifecycle

A scenario selects a compiled program, protocol configuration and arbitrary handler
data. `lmmock wire exec KIND --config FILE -- CLIENT ARG...` runs the client against
an ephemeral loopback listener and closes it when the client exits. LMMOCK_URL and
LMMOCK_ADDR identify that listener. `wire serve` provides readiness JSON for managed
processes. There are no sleeps or timeouts as mock behavior: handlers use receive,
send and peer completion to establish order.

### Handlers and extension path

Use `lmmock wire schema KIND` for the exact schema and the raw handlers in
[examples/wire](examples/wire). WebSocketHandler, HTTP2Handler, GRPCHandler and
LineHandler receive a Session; `bind` connects method/path keys to annotations.
See [the protocol extension contract](docs/wire-protocols.md) for compilation,
descriptor pins, stream semantics, the Session API and a complete line-protocol
example showing how to add the next listener plus SAM. Protocol/library limitations
and the separate TLS CLI integration boundary are stated there.

### Client acceptance

Go tests exercise real WebSocket and descriptor-driven gRPC clients, HTTP2 duplex
streaming and a locally trusted TLS listener. Wrong application/raw frame order,
descriptor mismatches, bad cardinality and missing flushes are refused. The dedicated
`lmcode protocol clients` Actions job installs the npm compiler privately and runs
these clients and production mutation controls; the regular suite includes them too.

The optional lminspect v0.1.0 recapture job runs on Linux: that release logs the
requested address before binding and does not report an allocated port. Its
Python harness requests port zero and discovers the actual listener through the
owned child PID's procfs socket descriptors. It never releases a reserved port or
accepts an unrelated process's listener as readiness. This Linux capture-job
restriction does not affect the native mock or provider fixtures on macOS/Windows.

## MCP over HTTP fixtures

lmmock includes scriptable current and legacy MCP HTTP fixtures. See
[`docs/mcp-http.md`](docs/mcp-http.md) for transport-version scope, controls,
commands, schemas, and consumer pin guidance. These examples use synthetic
state only and bind to loopback.
