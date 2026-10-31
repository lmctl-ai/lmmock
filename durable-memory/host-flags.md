# Host flag cutover

Discovery uses --lmcode-schema --json; serve/exec accept --lmcode for bytecode.
The Go flag declaration itself is named lmcode, so neither single- nor double-
dash retired execution options survive. No new signature-discovery command was
invented: the existing surface is preserved. DSLB and mock.dslb remain unchanged.

Actual parser tests prove retired schema/serve/exec modes refuse before input or
audit effects, exact new schema bytes, and the new input-failure route. Exec's
negative control supplies a child command so unrelated missing-command usage
cannot hide an accepted old flag. Every control fails with the original main.go
through Go's source overlay and passes after. The full normal gate passes all Go,
process/loopback/protocol/runtime JSON and 60 real Vercel-client scenarios.

Go module resolution pins SDK 65f0ef2; tracked and exact resolved-module source
spelling scans pass. No legacy alias, paid endpoint, global install or release
operation is used. The existing schema command and normal protocol server modes
remain available independently of the renamed host flag family.
