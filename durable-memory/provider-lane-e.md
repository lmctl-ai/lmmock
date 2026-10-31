# Print/sidecar provider boundaries

P0 schema_version=1 manifests under fixtures/providers/lane-e/manifests reuse the
shared validator/process executor/checker. The case runner downloads no vendor CLI:
it copies the candidate native lmmock into a private fake bin and injects an
absolute guarded bindings manifest. No symlinks/global aliases or real fallback.

RPC stdio schema 2 adds write_stderr, exit and make_directory, all generic process
primitives; schema 1 remains frozen/accepted. Existing framing, registered methods,
SAM validation and counters remain. stdio once/rpc require explicit custom bytecode.
HTTP schema 5 adds append_zstd/read_zstd for local sidecar record framing; prior
HTTP snapshots remain accepted. New program policy stays in lmcode.

Agy/Pi print stdout has no structured token facts; assert unavailable, not zero.
lmplayer's actual adapter is run --format json, with native session/model argv and
stdout error events/nonzero exits. DSH writes the observed flat session header and
seq-scoped turn/message/end records as actual concatenated zstd frames before RPC
completion. Cookie/v2 slash methods are distinct data cases. Muse follows actual
MSP UUID/home/workspace/admission/item/usage/terminal contracts and persists native
JSONL events. Real lmctl export verifies 5 input, 2 output, 1 reasoning; resume
adds another owned 5/2 without recreation or double counting.

Native print, DSH reply, Muse terminal and sidecar boot mutations compile then
fail their intended actual-client/IO assertions. No delay/hang/deadline fixture.
Existing client polling/lifecycle guards are not added mock behavior.

Published 0.2.448 currently accepts zero-exit empty Agy/Pi output and empty lmplayer
JSON as ok/empty. This is diagnosed, retained in a separate qualified=false evidence
record, and never counted as successful fixture coverage. The mock's nonzero/malformed
adverse cases qualify; a downstream client fix is still required for the zero-exit
empty variants. Never weaken the successful-reply oracle or fabricate token/cost data.
