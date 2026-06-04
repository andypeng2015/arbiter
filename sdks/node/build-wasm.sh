#!/usr/bin/env bash
# Build the WASM bundle that powers the local-eval SDK (src/local.js).
#
#   ./build-wasm.sh   # produces ./wasm/{arbiter.wasm,wasm_exec.js,loader.js}
#
# These are gitignored build artifacts; run this before `require(".../local")`.
set -euo pipefail
cd "$(dirname "$0")"

repo="$(git rev-parse --show-toplevel)"
mkdir -p wasm

echo "building wasm/arbiter.wasm…"
GOOS=js GOARCH=wasm go build -o wasm/arbiter.wasm "$repo/cmd/arbiter-wasm"

echo "copying wasm_exec.js + loader.js…"
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" wasm/wasm_exec.js
cp "$repo/cmd/arbiter-wasm/loader.js" wasm/loader.js

echo "done — local eval is ready (require('@arbiter/sdk-node/local'))."
