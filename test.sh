#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec "${LMSCRIPT_BIN:-lmscript}"  test.lms "$@"
