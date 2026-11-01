# Provider fixture contract

`SCHEMA.json` defines version 1 manifests. Each manifest pins one wire dialect and
version, its evidence source, model-family IDs/capabilities, the lmcode handler and
config, declared route-to-SAM annotations, request cases, and deterministic faults.
The `openai-chat.json` file is the reference manifest. It deliberately uses the
existing `examples/llm.lms` handler rather than duplicating its provider behavior.

Case kinds are `success`, `error`, `usage`, `model`, `tool`, and `stream`. HTTP
requests are JSON data, never shell strings. Process requests use an executable plus
argv, env and optional stdin; the runner uses `spawn` without a shell. Expectations
can assert status/headers/body JSON, model, token usage, tool calls, ordered stream
events, process exit/stdout/stderr, `error_class`, and file effects. File effects
resolve beneath the run's private temp directory. Placeholders are data substitutions
(`{{base_url}}`, `{{lmmock_url}}`, `{{repo_root}}`, `{{tmpdir}}`), not shell expansion.
For a process case, the declared `server.url_env` is populated with the loopback URL;
provider-specific overrides can add other env entries explicitly. Use a Node entrypoint
as `argv[0]` for npm-installed JS CLIs on Windows, where npm's `.cmd` shim is not a
shell-free executable.

`FAULTS.json` is the stable fault vocabulary. Each manifest fault ties a catalogue
fault to a named case and deterministic request/model selector. It has no delay,
sleep, retry clock, or timeout field. Fixture cases may intentionally return malformed
wire data; their expectation must state the failure observable at the tested client
boundary. For an lmctl probe, assert its emitted failure class instead of mapping an
HTTP status directly to a product class.

Run from the repo root after building the binary and compiling the declared handler:

```sh
node tests/provider-fixtures/run.mjs fixtures/providers/openai-chat.json build/lmmock
```

The runner starts an HTTP handler with `lmmock serve`, waits for its readiness
record, executes cases in manifest order, and sends SIGTERM only to that child when
finished. It has no per-case timeout. `tests/provider-fixtures/contract.test.mjs`
validates every manifest, executes HTTP profiles, and leaves non-HTTP lifecycle
execution to the corresponding protocol-owned process suite; both use this same
manifest contract. It mutates a successful expectation and an lmcode handler response
to prove both are caught, exercises shell-free process assertions, and refuses
malformed manifests before opening the server.

Source kinds distinguish published public specs, SDK type definitions, captures and
synthetic-only contracts. Capture provenance must explain whose client was captured
and whether the traffic was local synthetic data. Captures needing an operator-owned
client or credential are labeled `capture`/“capture needs operator”; tests must not
read or store credentials. All tokens in checked-in examples are synthetic.

JSONL stdio manifests use the protocol-specific published-client adapter in
tests/streamjson/run.mjs through the shared loader/expectation checker; no HTTP
server is started for native CLI fixtures. See fixtures/streamjson/README.md for
the isolated fake PATH and pinned parser limitations.

Provider text streams can declare `expect.stream` with dialect, text, input_tokens
and output_tokens. The shared strict wire decoder validates JSON, ordering,
terminal markers, decoded text and integer usage. It supports the canned text
streams of Anthropic, Gemini, Ollama and Miniplayer; it is not a general SDK
implementation. The provider-compat lane separately parses success and malformed
usage/JSON with the pinned Anthropic, Google and OpenAI-compatible SDKs. Ordering
and missing-terminal rejection are strict wire checks, not claims about SDK EOF
enforcement. Ollama uses the documented native JSON/NDJSON contract directly.
The Ollama fixture also asserts `/api/chat` tool definitions, function-name and
argument tool calls in JSON and NDJSON, tool-result history, `/api/tags`, and
deterministic auth/rate/server/model errors.
