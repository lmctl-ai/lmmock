# Protocol-layer extension contract

## Division of responsibility

Go adapters own listeners, framing, TLS integration and protobuf conversion.
The engine loads a program, indexes `entries()` by annotation, runs its binding
body once and calls the selected SAM entry for a conversation. New applications,
routes, method behavior, validation and ordered responses belong in lmcode.
There are no application method switches or reply tables in the Go adapters.

`internal/wire.Layer` exposes Scheme, ValidateRoute and Serve. Its factory receives
a scenario directory and protocol-only JSON. `wire.Register` declares the kind's
SAM name, makes its schema available, and connects it to the existing proto registry.
This is compiled-in extensibility, not dynamic Go plugins: new kinds require a
binary rebuild; new applications on a kind do not.

`wire.Session` exposes receive/send, metadata and failure framing. Receives end
on peer half-close; sends flush before returning. The engine provides per-call
VM registries/arenas and synchronized service-owned string state and counters.
A new engine/process resets state. No sleeps, deadlines or timed scenario faults
are available. Connections close on cancellation, including hijacked sockets.

## Worked extension: LF command protocol

`internal/wire/line/line.go` is the complete runnable example. It registers
LineHandler, validates the `connection` binding, accepts clients, reads LF-terminated
frames with bufio.Reader, and writes one LF-terminated frame per send. It contains
no PING/PONG policy. The application is `examples/wire/line.lm`:

```text
@commands
fn commands(s: Session) -> void implements LineHandler {
  while (s.recv()) {
    if (s.body() == "PING") { s.send("PONG"); }
    else { s.send("ERR " + s.body()); }
  }
}
bind("connection", "commands");
```

A different greeting, authentication exchange or command set changes this handler,
not Go. A database/SMTP adapter follows the same steps: implement its actual wire
framing in a new package, implement Session, register its SAM and factory, and add
one blank import in a new cmd/lmmock file. Validate route/configuration shape before
listening; unblock receive/send on cancellation; add a real client test. Do not
implement database query semantics or SMTP admission policy in a Go switch.

## Scenario files and compilation

Scenarios contain `program`, `protocol` and `data` only. Paths in `wire serve/exec`
are relative to the scenario file. The legacy `proto KIND` entrypoint also exposes
these adapters but resolves paths against cwd; use absolute paths there.

```json
{"program":"chat.dslb","protocol":{"subprotocols":["fixture-v1"]},"data":{}}
```

Export a kind schema with `lmmock wire schema ws`; the standard query form is
`lmmock wire ws --lmcode-schema --json`, and `--lmcode-signatures` lists the registry.
Save the schema next to the handler. From a checkout/examples archive, use
`node examples/wire/compile.mjs SCHEMA.json HANDLER.lm PROGRAM.dslb` with the
repository's npm compiler pin (0.2.448), optionally selected by LMSCRIPT_BIN.
The adapter uses that published compiler's existing compile-only surface solely
to materialize fixtures; it does not invent a language export alias or promise
compatibility with an unpublished compiler cutover. Existing repository source
compilation has the same pin. Precompiled serving needs only the Go binary.

Run `lmmock wire exec ws --config scenario.json -- CLIENT ARG...` to own the server
for a client lifetime; the child gets LMMOCK_URL and LMMOCK_ADDR, and its exit code
is preserved. `wire serve` prints readiness JSON and waits for cancellation.
Both commands reject non-loopback listeners. Source and scenarios are trusted;
lmcode can use its standard file operations and is not a sandbox.

## Session surface

| Operation | Meaning |
|---|---|
| `bind(key, annotation)` | Startup-only binding; duplicates/missing annotations/wrong SAM fail before listening. Exact bindings win over an explicit `*` fallback. |
| `recv()` | Receive the next protocol message; false on EOF. HTTP2 has one complete request-body event. |
| `body()`, `frame()`, `method()` | Last received payload/kind and actual routing key. |
| `send(text)` | One ordered text/protobuf-JSON/HTTP DATA/line event, flushed before return. |
| `send_binary(base64)` | WebSocket binary message; other adapters refuse it. |
| `header(name)` | Request metadata. HTTP adapters expose :method, :path (including query), :authority/Host and :scheme; WS also exposes selected :protocol. |
| `set_header(name,value)` | HTTP2 response headers before data, or gRPC metadata; transport-owned/invalid metadata is refused. WS handshake headers are already sent. |
| `status(code)` | HTTP2 final status 200..599, before data; other kinds refuse. |
| `fail(code,message)` | Abort the handler with a WS close code, HTTP error status, or gRPC status 1..16. A failure after HTTP DATA resets the stream. Line failures close the connection. |
| `config()`, `state_get`, `state_put`, `count` | Scenario data JSON and service state; count increments atomically. |
| `json_quote(text)` | Generic UTF-8 JSON string encoding for constructing responses. |

## gRPC descriptor contract

The scenario supplies a FileDescriptorSet and its SHA256. Default encoding is
binary; `descriptor_encoding: "json"` accepts protobuf JSON for the descriptor set.
Include imports in the set; no descriptor or type URL is fetched from the network.
`examples/wire/echo-descriptor.json` is a synthetic service with unary, server,
client and bidirectional streams. Pin the exact bytes, not normalized JSON.

```json
{
  "program":"grpc.dslb",
  "protocol":{
    "descriptor_set":"echo.pb",
    "descriptor_sha256":"REPLACE_WITH_THE_EXACT_FILE_SHA256"
  },
  "data":{}
}
```

Bind `/package.Service/Method` to an annotated GRPCHandler. A `*` handler may cover
methods present in the supplied set; methods absent from it remain Unimplemented.
Input/output types and stream cardinality come from descriptors. Unary/server-stream
input is decoded and validated before dispatch. Streaming handlers control how much
they receive; returning ends the response stream without requiring peer half-close.
Non-server-streaming methods must emit exactly one response.

Payloads use protobuf JSON: int64 values are strings, bytes are base64, and
non-optional scalar defaults/empty lists/maps are emitted. Presence-aware omitted
fields stay omitted. Dynamic Any/extension resolution uses the scenario's local
type registry, never global application registrations. Received unknown fields,
including decoded Any payloads, and incompatible response JSON fail loudly.
A pin mismatch or unknown bound method fails startup. Wire-compatible client schema
mistakes cannot be inferred from field numbers alone; the pin protects the fixture
schema, not an unverifiable claim about the client's source schema.

Tests use grpc-go dynamic messages and method paths, like a client supplied with
a protoset rather than generated application stubs. Reflection, grpc-web and HTTP
transcoding are not implemented. The network APIs accept wrapped TLS listeners;
HTTP2 TLS is tested with a private trust root. CLI certificate generation/trust-file
convenience belongs to the separate shared TLS layer. WebSockets are RFC 6455
upgrades, not HTTP2 extended CONNECT; the standard same-origin check is retained.

## Acceptance controls

`go test ./internal/wire/...` runs real WS, HTTP2 and gRPC clients, including ordered
request/response handshakes, binary frames, compression, defaults/Any, cardinality,
unknown fields, SHA mismatch, wrong SAM and new-method-without-Go cases. HTTP2 uses
an IO-pipe gate: the client supplies the next input only after observing the flushed
first event. No wall-clock delay is used to establish order.

`tests/wire/controls.py` runs baselines and production-copy mutants: omitted flush,
swapped response order, ignored descriptor pin, missing pre-dispatch unary validation,
extra unary response, and omitted defaults. Build failures do not count as catches.
The normal test.lms and GitHub Actions run the client suite and these controls.
