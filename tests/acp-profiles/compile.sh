#!/usr/bin/env bash
set -euo pipefail
mkdir -p build
node scripts/capture-fixture.mjs  fixtures/acp/handler.lms build/acp-profiles.lmcode
extension=""
if [[ "$(go env GOOS)" == windows ]]; then extension=".exe"; fi
go build -o "build/acp-shim$extension" ./tests/acp-profiles
