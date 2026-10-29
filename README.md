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
