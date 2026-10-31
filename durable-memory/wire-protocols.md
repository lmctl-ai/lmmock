# Protocol adapters and lmcode dispatch

The network extension API is internal/wire.Layer + Session. Definition registration
supplies the SAM and factory, joins the existing proto registry, and exposes schema
introspection. Engine owns program bytes, annotation indexing and synchronized state;
registries/VM arenas are per conversation. Go adapters contain no application replies.

WebSocket uses Gorilla v1.5.3; HTTP2 uses Go 1.26 net/http protocols; gRPC v1.85.0 uses
scenario-pinned FileDescriptorSet types with protobuf v1.36.12. Exact permissive
licenses are in THIRD_PARTY_NOTICES.md. CGO=0 remains the release build rule.

Use docs/wire-protocols.md as the current contract. Raw fixture chunks are in
examples/wire; the published 0.2.448 compiler adapter is fixture tooling, not a
new public export function. New compiler package migration must move the existing
repository fixture pipeline and this adapter coherently, with no silent fallback.

The tests prove ordered interactive frames without sleeps, HTTP2 flush-before-read,
TLS listener interoperability, gRPC input validation before unary dispatch, independent
stream completion, local Any resolution and descriptor/cardinality refusal. Generic
protocol state lives in the server engine. Do not add domain methods to Go: add a
binding/handler; add Go only for genuinely new framing or transport capabilities.
