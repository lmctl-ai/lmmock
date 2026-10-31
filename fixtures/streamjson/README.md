# Claude/Qwen native stream-json fixtures

One annotated lmcode policy serves both dialect manifests. Model IDs/families are
data in fixtures/providers; they are representative selectors, not availability
or vendor-capability promises. The source is lmctl's adapter boundary plus the
pinned published client, not a real vendor capture. No provider CLI is downloaded
or executed: tests copy the candidate Go executable under claude/qwen names in a
private PATH. Native executables work on Windows without shell or cmd shims.

The new generic JSONL layer reuses the existing RPCHandler SAM and checked line
framing. `$start` forwards to annotated version/seed/open functions; `$message`
selects an explicit turn cell. Go recognizes no provider, model or event name.
The environment fields LMMOCK_JSONLINES_PROGRAM and LMMOCK_JSONLINES_SCENARIO
opt an owned child into this layer; optional ALIAS/STATE suffixes use the existing
private counter facility. jsonlines --schema prints the host schema. No upstream
forwarding, sleep, delay, timeout, retries or fabricated terminal is added.

wire.json is source data, not a ready-to-run scenario. The fixture client renders
its model/session placeholders and private audit/native-store paths into a per-case
configuration with version, seed, init and turns. Each cell explicitly declares
its events, close behavior, optional diagnostic, and owned transcript effects.
Result-bearing cells wait for input EOF, which the real client sends after
processing a result. Missing-terminal faults explicitly close instead. This avoids
Windows exit-versus-stdout races without a mock grace timer. A dropped-terminal
mutant also closes, matching the finite missing-result fault rather than creating
a two-sided wait for an acknowledgement that can never arrive.
Unknown turns refuse. Source handler extensions need lmcode, not Go switches.

Tests use the frozen P0 manifests and shared expectation checker. They run the real
npm 0.2.448 lm_seed, lm_prompt, session reader/export, verbose CLI prompt and batch
adapters in isolated HOME/USERPROFILE/XDG/database directories. Pricing is pinned
to that npm package's bundled dataset, so acceptance makes no network calls.
Synthetic native JSONL files are created by the seed handler, then appended by turn
handlers. Session pinning, exact argv, JSONL user messages, ordered content/thinking/
tool events, stdout/stderr/exit and native usage are independently checked.

Claude wire counters are input 7, cache read 3, cache write 5 (5m 2 + 1h 3), output
11 and thinking 4. Native export verifies its cache buckets. Qwen's native
usageMetadata reports prompt 15 including cache 3, output 11 including thoughts 4;
its real session reader/export verifies these. Qwen's stream adapter in this pin
does not expose wire usage, so a null source/unavailable wire bill is asserted,
never invented as zero. Actual CLI progress retains tool-use/result order and
keeps raw thinking text private. Two-turn default batch uses one startup and two
JSONL user requests, with distinct terminal identities and no timing barriers.

## Pinned parser limits

The requested universal duplicate/malformed-usage rejection is not implemented by
this published client. Same (session, uuid) terminals are deliberately deduplicated;
different UUID terminals accumulate as additional responses. Malformed Claude
counters leave status ok/error_class null, an unknown counter and unavailable cost.
Qwen's wire usage remains unavailable even when it emits usageMetadata. These are
explicit fixtures with error_class null under the generic JSON expectation; they
are not claimed as rejected errors. Missing final result yields the real
error_class unknown, and explicit invalid-key result/process failures yield
auth_required. Do not fabricate an error inside the mock to hide a client gap.

Run after a private npm install and candidate build:

```
LMSCRIPT_BIN=/private/npm/node_modules/.bin/lmscript bash scripts/compile-streamjson.sh
LMMOCK_LMCTL_PACKAGE=/private/npm/node_modules/@lmctl-ai/lmctl node tests/streamjson/run.mjs build/lmmock
LMMOCK_LMCTL_PACKAGE=/private/npm/node_modules/@lmctl-ai/lmctl node tests/streamjson/controls.mjs build/lmmock
```

The regular test.lms invokes the shared manifest validation gate, then the
protocol-owned published-client suite, direct native process checks and compiled
handler controls. The HTTP fixture runner does not launch stdio handlers. Dedicated
Actions downloads the pinned npm package and qualifies all five native platforms.
A dropped-terminal lmcode mutant must compile, then fail at the real adapter's
unknown-error result for each dialect. Changed expected error classes must also
fail. Crashes, compilation errors and missing dependencies are not catches.
