#!/usr/bin/env bash
set -euo pipefail
for name in api faults stateful; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "examples/$name.lms" "build/$name.dslb"
done
