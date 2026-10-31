#!/usr/bin/env bash
set -euo pipefail
for preset in acp mcp mcp-client codex claude gemini agy; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "internal/rpcstdio/presets/$preset.lms" "internal/rpcstdio/presets/$preset.dslb"
done
