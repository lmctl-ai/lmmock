#!/usr/bin/env bash
set -euo pipefail
# npm's Windows launcher must be interpreted by bash, not spawned as a native exe.
node scripts/capture-fixture.mjs  fixtures/openai/handler.lms build/openai.lmcode
