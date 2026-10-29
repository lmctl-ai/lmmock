#!/usr/bin/env bash
set -euo pipefail
for name in api faults stateful oauth connector-http llm; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "examples/$name.lms" "build/$name.dslb"
done
