# Codex app-server coverage

The canonical policy source is fixtures/codex/handler.lms; compile-stdio.sh embeds
it as the codex preset. RPCHandler and the generic stdio host are unchanged. The
former duplicate preset source was removed. Model/fault data and provenance use
the frozen P0 manifest contract; no per-model Go branches were introduced.

See fixtures/codex/README.md for exact gates and the three unfulfilled published
client requirements: specialized malformed-usage classification, specialized
foreign-terminal classification, and reliable cancellation accounting settlement.
These are explicit strict assertion controls, not inferred HTTP error classes.
Published0.2.448's package version is asserted independently from its observed
initialize.clientInfo.version fallback0.0.0. Never rewrite that wire field to
pretend a better client was tested.

The protocol-owned Codex runner validates manifests through the shared P0 schema
and reuses its executeProcess/checkExpectation helpers. The shared HTTP runner
retains its explicit HTTP ownership boundary. The shared process-suite resolves the declared node harness to the actual Node
executable after checking its real path is under tests/. No bare provider command
is launched by the manifest runner. Existing HTTP readiness remains unchanged.

The cross-platform cancellation rendezvous observes the real child stdout at client
delivery, without replacing the process or changing its bytes. Notification runs
after the client data callbacks and promise jobs, then IPC triggers the published
runner signal handler. Filesystem hints can be missed; server-write observation
can precede Windows pipe delivery and send cancellation before a turn ID exists.
The test-only observer adds no elapsed-time delay or mock policy.
