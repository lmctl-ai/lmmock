#!/usr/bin/env bash
set -euo pipefail
# npm's Windows launcher must be interpreted by bash, not spawned as a native exe.
"${LMSCRIPT_BIN:-lmscript}" --no-preflight fixtures/openai/handler.lms build/openai.dslb
