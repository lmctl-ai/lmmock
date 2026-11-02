# Current lmcode invocation protocol

SDK pin: lmcode-go 06cd9ae991d9 (2026-11-02 origin/main). The HTTP exec/serve
entrypoint constructs InvocationResultsFromCommand before reading a program.
Invalid invocation metadata fails with a CLI envelope (exit 2); load/run errors
use exit 3, and a failed client under run_lmcode uses an application envelope
(exit 4). Select uses the schema ResultContract, then Complete/WriteFrame emits
one terminal frame after server shutdown. Standalone exec preserves child exit
codes. Under invocation metadata, client stdout is captured as result.stdout (10 MiB cap), with result.code = 0.
The script can print that field to reproduce exact client output; stderr remains stderr. The child does not inherit LMCODE_INVOCATION_FILE.

Dry metadata verifies the program without running its body, opening listeners,
writing readiness/audit files, or spawning clients. Discovery (schema and
--lmcode-schema --json) never consumes invocation metadata.

Source programs use run_lmcode with a command template containing $DSLFILE.
Lmscript creates/deletes that private file; there is no source export API.
The test/build-only scripts/capture-fixture.mjs supplies scripts/capture-host.mjs
as a trusted invocation host and captures its input for embedded stdio presets,
wire tests and byte-level mutation controls. Captured .lmcode files are internal
build artifacts, not a new stable public compiler format. Do not expose the
capture host to untrusted source; bytecode is trusted host code.

Existing workflow compiler pins are 0.2.454; test.sh uses LMSCRIPT_BIN when set.
Published client tests also require the corresponding npm .bin directory on PATH
and LMCTL_PACKAGE_ROOT pointing to its package. npm ci --prefix tests/vercel is
required for real Vercel SDK client tests. No API keys are required.
