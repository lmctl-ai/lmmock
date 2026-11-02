#!/usr/bin/env bash
set -euo pipefail
for name in missing duplicate conflict no-route runtime-error not-found legacy; do
  node scripts/capture-fixture.mjs  "build/$name.lms" "build/$name.lmcode"
done
