#!/usr/bin/env bash
set -euo pipefail
# npm's Windows launcher must be interpreted by bash, not spawned as a native exe.
for d in anthropic gemini ollama miniplayer; do
  node scripts/capture-fixture.mjs  "fixtures/$d/handler.lms" "build/$d.lmcode"
done
