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

The runner starts the declared handler with `lmmock serve`, waits for its readiness
record, executes cases in manifest order, and sends SIGTERM only to that child when
finished. It has no per-case timeout. `tests/provider-fixtures/contract.test.mjs`
also mutates a successful expectation and proves the runner rejects the changed
expectation; malformed manifests are refused before opening the server.

Source kinds distinguish published public specs, SDK type definitions, captures and
synthetic-only contracts. Capture provenance must explain whose client was captured
and whether the traffic was local synthetic data. Captures needing an operator-owned
client or credential are labeled `capture`/“capture needs operator”; tests must not
read or store credentials. All tokens in checked-in examples are synthetic.
