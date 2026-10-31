#!/usr/bin/env bash
set -euo pipefail
mkdir -p build
"${LMSCRIPT_BIN:-lmscript}" --no-preflight fixtures/acp/handler.lms build/acp-profiles.dslb
extension=""
if [[ "$(go env GOOS)" == windows ]]; then extension=".exe"; fi
go build -o "build/acp-shim$extension" ./tests/acp-profiles
