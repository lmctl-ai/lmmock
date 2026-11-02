#!/usr/bin/env bash
# Regenerate the bytecode embedded in the lmmock binary (--builtin llm-api).
# Needs lmscript (npm @lmctl-ai/lmctl, pinned in release.yml) on PATH or LMSCRIPT_BIN.
set -euo pipefail
cd "$(dirname "$0")/.."
"${LMSCRIPT_BIN:-lmscript}" --no-preflight examples/llm-api.lms cmd/lmmock/builtin/llm-api.dslb
