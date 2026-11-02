#!/usr/bin/env bash
# Regenerate the bytecode embedded in the lmmock binary (--builtin NAME).
# Every examples/NAME.lms listed below becomes cmd/lmmock/builtin/NAME.lmcode.
# Needs lmscript (npm @lmctl-ai/lmctl, pinned in release.yml) on PATH or LMSCRIPT_BIN.
set -euo pipefail
cd "$(dirname "$0")/.."
for name in llm-api oauth-codex-device; do
  node scripts/capture-fixture.mjs "examples/$name.lms" "cmd/lmmock/builtin/$name.lmcode"
done
