#!/usr/bin/env bash
set -euo pipefail
mkdir -p build
for profile in print muse dsh sidecar-boot; do
  node scripts/capture-fixture.mjs  "fixtures/providers/lane-e/$profile.lms" "build/lane-e-$profile.lmcode"
done

node scripts/capture-fixture.mjs  internal/rpcstdio/testdata/process-output.lms internal/rpcstdio/testdata/process-output.lmcode
