#!/usr/bin/env bash
set -euo pipefail
"${LMSCRIPT_BIN:-lmscript}" --no-preflight fixtures/streamjson/handler.lms fixtures/streamjson/handler.dslb
