# lmmock notes

Go HTTP mock host backed by lmcode-go v0.6.37. Public repo lmctl-ai/lmmock, MIT.

- **Start here for new HTTP services:** [Handler implementation and n8n expansion workflow](adding-n8n-http-services.md). Includes a working annotated Handler, language constraints, n8n source inspection, suite registration, lmconn fixture sync and Actions verification.

- `internal/mock/schema.json` is the exact embedded compiler schema. Keep `examples/schema.json` byte-identical; schema changes require recompilation.
- `@annotation fn name(x: Exchange) -> void implements Handler` entries are bound by top-level `route(method,path,annotation)`. Body runs only at startup. Startup validates every entry, binding and ServeMux route before listening.
- `internal/mock/mock.go` owns HTTP, shared state and VM adapters. Per-request registry/program loading avoids SDK registry run-state sharing. State operations are mutex-protected; increment is atomic.
- `cmd/lmmock` implements source/bytecode modes, loopback-only listeners, readiness, signal shutdown and `exec` client lifetime. Source mode invokes current npm lmscript, including Windows shim resolution. Scripts are trusted, not sandboxed; VM loops have no instruction deadline.
- `test.lms` compiles all examples/negative fixtures, runs Go tests, builds with CGO disabled, and runs Node real-process/client integration checks. No Python test driver. `go test` requires those generated build fixtures.
- Actions tests five native platforms and a Linux race check across all internal packages. Only successful main builds publish binaries, example archive, licenses and checksums.
- Workspace operator files (`*.lmc`, p1.txt, p2.txt) are local and intentionally excluded from the public repository.

- OAuth/LLM enhancement: helpers.go supplies general configuration, form/crypto and atomic take primitives. examples/oauth.lms implements strict grant policy; examples/llm.lms implements deterministic OpenAI chat/tool/SSE fixtures. scripts use a single-pass while/break for early exits because lmcode does not allow early returns.
- `lmmock check --suite FILE [--url URL]` is a native HTTP assertion client with captures, dot-path JSON assertions and redacted summaries. tests/protocols.mjs tests protocol failures and replay races. Linux Actions checks released lmcurl v0.1.2 using an isolated v5 compiler (0.2.426); current compiler still builds lmmock v6 scripts.
- [n8n connector auth shapes](n8n-connector-auth-shapes.md): what the Slack/Discord/Telegram/Jira/Stripe presets taught about credential tests, auth header shapes, vendor error shapes and fault routes.
- [lmconn coverage review](../docs/lmconn-coverage.md): actual five command areas, new vendor/GitHub integration tests, and remaining auth/protocol gaps. Protocol CLI now rejects non-loopback addresses before readiness; Actions race-tests all internal packages.

- [Expanded HTTP presets](../docs/http-presets.md): Notion, Airtable, HubSpot, GitLab, Asana and Trello use annotated Handler methods; source scripts plus shared native/lmconn suites cover auth, cursor pagination, create/read state and faults. Lists are fixed snapshots.

- [Uncovered n8n connectors](../n8n-connectors-uncovered.md): manifest-based backlog for both official base and AI node packages, with pinned source links, partial service/protocol coverage and utilities separated. Refresh this list when adding presets.

- [Commerce/publishing presets](../docs/http-presets.md#commerce-and-publishing-presets): Shopify, WordPress and WooCommerce source-derived credential checks, two-page header-based pagination, create/read state and credential overrides. `tests/commerce.mjs` additionally checks pagination termination and overrides. Protected-site WordPress policy and omitted vendor operations are explicit.

- [LLM API acceptance](../docs/llm-api.md): llm-api.lms implements OpenAI Chat/Responses/models/embeddings, native Anthropic Messages and Gemini generateContent/streamGenerateContent/embeddings with annotated handlers. Real Vercel SDK acceptance runs from test.lms. LiteLLM was only an architectural reference; do not add LiteLLM runtimes or compatibility suites. Install npm ci --prefix tests/vercel before the main suite. Legacy llm.lms remains for credential-redaction tests. SSE remains buffered. Sixty real SDK acceptance scenarios cover five provider paths; Anthropic uses x-api-key plus anthropic-version, Gemini uses x-goog-api-key or query key. Vercel is only the sample client; the goal is the three native API formats.

- [Runtime JSON data flow](../docs/runtime-json.md): response objects are constructed in lmscript, passed through existing interpolation into lmcode, then rendered with json_render(Json, Json) using a nested context map. The reusable Go implementation is pkg/jsontemplate.Render; <path> placeholders preserve whole-value JSON types, support dotted paths and array indexes, and use << for literal <. It never reinterprets inserted data. json_with(Json, JSON-Pointer, Json) supports immutable updates. See the runtime guide and package tests for the contract to promote into lmcode std functions. This is response data, not schema.json. --script captures values at startup; --dsl uses previously captured values. No Go rebuild for data changes. LLM responses now contain no handwritten escaped JSON. Host schema v2 accepts frozen v1 bytecode; schema is still explicitly maintained, not reflection-generated.
