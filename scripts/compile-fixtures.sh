#!/usr/bin/env bash
set -euo pipefail
for name in missing duplicate conflict no-route runtime-error not-found; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "build/$name.lms" "build/$name.dslb"
done
