#!/usr/bin/env bash
set -euo pipefail
for preset in acp mcp mcp-client claude gemini agy; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "internal/rpcstdio/presets/$preset.lms" "internal/rpcstdio/presets/$preset.dslb"
done

"${LMSCRIPT_BIN:-lmscript}" --no-preflight fixtures/codex/handler.lms internal/rpcstdio/presets/codex.dslb
