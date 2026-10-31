# Stdio protocol boundary

internal/rpcstdio owns framing, entry validation/dispatch, IDs, stdout flushes and
per-process state/counter plumbing. Presets in its presets directory own ACP, MCP,
client workflows and agent wire formats. Method/tool extension is bind + an
annotated RPCHandler; forward defers a new entry until the previous VM call drains,
so no VM arena/registry is recursively entered. No Go switch on protocol methods.

scripts/compile-stdio.sh regenerates the embedded bytecode using published npm
compiler 0.2.448. The source schema and lmcode files use LF; Actions checks compiled
preset drift. Keep these bytes coherent with the embedded schema. Runtime uses
embedded programs; --lmcode explicitly replaces one with a custom program.

Unit tests and tests/stdio/{process.mjs,controls.py,lmctl.mjs} prove malformed
framing/refusals, streaming, stop reason, alias counters, errors/empty cells,
MCP resources/prompts/tools and client ID correlation. Controls must compile and
fail named assertions. No compiler failure or missing-test selection is a catch.

Only published ACP client paths are integration-qualified with lmctl. Native
Claude/Codex/Gemini/agy session discovery/store behaviors are not recreated. agy
uses plain print output, not JSON events. The service-owned lmbee MCP topology has
no entrypoint in tested npm lmctl; do not claim standalone peer tests cover it.
