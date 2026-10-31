# Print and sidecar provider fixtures

The manifests use the shared fixtures/providers/SCHEMA.json contract, frozen by
P0. Tests reuse its loader, shell-free process executor and expectation checker.
This lane has 18 qualified cases, with one success/adverse minimum for Agy, Pi,
lmplayer, DSH and Muse. They run the actual privately downloaded npm lmctl 0.2.448,
not a second parser. Native executable copies preserve Windows/Unix argv and exit
behavior; no installed vendor executable is used. Model families live in wire.json
and manifest model arrays, not one handler per model ID.

## Handler and transport boundaries

print.lms is an annotated RPCHandler startup program. It verifies model/prompt
argv from data, writes exact configured stdout/stderr and selects an exit status.
Agy and Pi use their print argv; lmplayer uses run --format json, including its
end-of-options delimiter and returned sessionID. Trace files confirm the real
adapter's final argv, including permission/model/session facts. Print counters are
not exposed by these wires and are asserted unknown, never fabricated as zero.

muse.lms uses the existing JSON-RPC transport with annotated methods. It implements
MSP version/home validation, initialize/initialized, start/resume, approval/model
selection, turn admission, item text delta, usage and terminal notifications.
Native JSONL metadata/events are persisted in the private fixture home. The real
lmctl session exporter confirms model/token facts after completion and resume.
Invalid terminals and missing terminal/EOF are refused by the actual MSP client.
The fixture is a documented subset, not the complete Muse service or tool API.

dsh.lms supplies HTTP RPC policy; sidecar-boot.lms supplies process startup output.
The generic sidecar mode owns only the listener, process pipes and lifecycle.
The handlers support dot/v1 and slash/cookie/v2 RPC, session create/selectModel/
prompt, actual concatenated zstd JSONL publication, explicit malformed envelopes,
boot failure and failed turns. Files are flushed before the prompt RPC returns;
there are no mock sleeps or poll timers. lmctl's own adapter may poll its files.
A two-process resume test proves the same session gains a second owned turn,
without creating the session again or losing token facts (5/2 becomes 10/4).

Only generic protocol primitives were added in Go: native process binding,
stdout/stderr/exit, sidecar listener ownership, directory creation and zstd record
framing. Provider methods, events, model handling and failures remain lmcode.
Known old stdio and HTTP schema snapshots remain accepted for existing fixtures.

## Invocation

The test runner constructs private per-case configuration from wire.json, which is
family data rather than a directly runnable complete server config. The shared
manifest's process request launches case.mjs; its output copies lmctl's own
errorClass into error_class for the shared checker and retains the raw CLI record.
It never infers a class from HTTP status. Error expectations are deliberately
mutated too, proving a wrong class fails. Raw native stdout/stderr/exit are checked
independently of the normalized client result.

Set LMCTL_TEST_PREFIX to the private npm installation prefix, or LMCTL_TEST_CLI to
the published dist/cli/index.js. Existing normal CI installs the pinned npm package.
Compile through scripts/compile-lane-e.sh before running the case suites.

```sh
node tests/providers/lane-e/run.mjs build/lmmock
node tests/providers/lane-e/controls.mjs build/lmmock
```

LMMOCK_PROCESS_BINDINGS is an explicit private map from owned executable names to
normal lmmock command prefixes. The real executable must remain inside the declared
absolute owned directory; unknown/trailing metadata and path escape are refused.
Copy the built executable into that directory; do not create provider-name aliases
in global PATH. stdio once runs one startup handler; stdio rpc runs JSON-RPC until
EOF/explicit close. sidecar composes startup RPCHandler and HTTP Handler programs.
No provider-name/model selection branch is added to the Go protocol dispatcher.

## Known published-client defects — not qualified

The immutable 0.2.448 client accepts zero-exit empty stdout for Agy/Pi and {} for
lmplayer as status=ok with an empty reply. The diagnostic suite proves that the
strict successful-reply oracle rejects those results and records qualified=false;
these are not counted among the 18 cases or presented as fixed. The evidence
artifact retains the raw observation. Fixing the published adapters requires a
separate lmctl change/release; this lane does not patch the client or disguise the
result. Nonzero exits, explicit failures and malformed DSH/MSP responses are
qualified and must never be accepted as successful empty replies.

Agy/Pi native seed/discovery databases, quota services, terminal-screen rendering,
PTY interaction, arbitrary lmplayer backends, concurrent same-session DSH writes,
and the complete Muse tool/cancellation API are not claimed. lmplayer run/JSONL
process contracts and its session/model argv are tested; this is not a graphical
TUI emulator. All prompts/credentials/models are synthetic; no provider config or
credential file outside the private fixture home is read. The suite uses only
loopback HTTP and owned stdio processes, with no real vendor calls.
