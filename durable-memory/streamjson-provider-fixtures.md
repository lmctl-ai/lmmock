# P1 D stream-json

Generic runtime: internal/rpcstdio/jsonlines.go and cmd/lmmock/jsonlines.go. An
explicit owned-child env selects native process emulation; common checked framing
and RPCHandler annotation dispatch remain in use. No Go provider/model policy.
fixtures/streamjson/handler.lms owns version/seed/open/user handlers; model selectors,
wire profiles and expected client classes live in the P0 manifest/data files.

Use npm 0.2.448 from tests/streamjson/client.json. run.mjs validates with the P0
loader/checker and injects only copied native binaries via private PATH. No shell,
credential store, provider download or vendor request. Price files are immutable
copies from that npm package. HOME, XDG, provider variables and DB are isolated.

Seed is an argv prompt (not JSONL), writes an owned native session and pins the
teamfile. Turns use JSONL and resume that id. Direct wire oracles check event order;
real API/verbose/batch and native export check text/tool/usage/session effects.
Persistent batch proves one startup/two prompts without sleeps or mock deadlines.

Do not strengthen observed classes by fabricating error events: missing result is
unknown; invalid-key errors auth_required. Same UUID duplicates are deduplicated,
distinct ones aggregate; malformed usage is accepted but its bill is unavailable.
Qwen wire usage is absent; native export verifies its Gemini-shaped metadata.
Those limits are explicit in the fixture README and expected JSON null fields.

Regular test.lms and streamjson-lmctl.yml run these with the shared provider gate.
controls.mjs compiles a real dropped-terminal handler for both dialects and mutates
expected classes; compile failures/crashes are not catches. Keep fixture bytecode
reproducible with scripts/compile-streamjson.sh. No new dependency or release step.
