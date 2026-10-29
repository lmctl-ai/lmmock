# lmmock notes

Go HTTP mock host backed by lmcode-go v0.6.37. Public repo lmctl-ai/lmmock, MIT.

- `internal/mock/schema.json` is the exact embedded compiler schema. Keep `examples/schema.json` byte-identical; schema changes require recompilation.
- `@annotation fn name(x: Exchange) -> void implements Handler` entries are bound by top-level `route(method,path,annotation)`. Body runs only at startup. Startup validates every entry, binding and ServeMux route before listening.
- `internal/mock/mock.go` owns HTTP, shared state and VM adapters. Per-request registry/program loading avoids SDK registry run-state sharing. State operations are mutex-protected; increment is atomic.
- `cmd/lmmock` implements source/bytecode modes, loopback-only listeners, readiness, signal shutdown and `exec` client lifetime. Source mode invokes current npm lmscript, including Windows shim resolution. Scripts are trusted, not sandboxed; VM loops have no instruction deadline.
- `test.lms` compiles all examples/negative fixtures, runs Go tests, builds with CGO disabled, and runs Node real-process/client integration checks. No Python test driver. `go test` requires those generated build fixtures.
- Actions tests five native platforms and a Linux race check. Only successful main builds publish binaries, example archive, licenses and checksums.
- Workspace operator files (`*.lmc`, p1.txt, p2.txt) are local and intentionally excluded from the public repository.
