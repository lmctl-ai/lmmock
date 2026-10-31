#!/usr/bin/env bash
set -euo pipefail
mkdir -p build
for profile in print muse dsh sidecar-boot; do
  "${LMSCRIPT_BIN:-lmscript}" --no-preflight "fixtures/providers/lane-e/$profile.lms" "build/lane-e-$profile.dslb"
done

"${LMSCRIPT_BIN:-lmscript}" --no-preflight internal/rpcstdio/testdata/process-output.lms internal/rpcstdio/testdata/process-output.dslb
