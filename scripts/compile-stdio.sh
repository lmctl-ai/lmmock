#!/usr/bin/env bash
set -euo pipefail
for preset in acp mcp mcp-client claude gemini agy; do
  node scripts/capture-fixture.mjs  "internal/rpcstdio/presets/$preset.lms" "internal/rpcstdio/presets/$preset.lmcode"
done

node scripts/capture-fixture.mjs  fixtures/codex/handler.lms internal/rpcstdio/presets/codex.lmcode

# Other committed programs bound to the same stdio host schema.
node scripts/capture-fixture.mjs  internal/rpcstdio/testdata/deferred.lms internal/rpcstdio/testdata/deferred.lmcode
bash scripts/compile-streamjson.sh
