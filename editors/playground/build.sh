#!/usr/bin/env bash
# Build the Arbiter WASM playground into this directory.
#
#   ./build.sh && python3 -m http.server 8080   # then open http://localhost:8080
#
# Produces (gitignored) build artifacts alongside index.html:
#   arbiter.wasm   — the compiled WASM module
#   wasm_exec.js   — Go's WASM runtime glue (from $GOROOT)
#   loader.js      — the Arbiter WASM SDK loader
set -euo pipefail
cd "$(dirname "$0")"

repo_root="$(git rev-parse --show-toplevel)"

echo "building arbiter.wasm…"
GOOS=js GOARCH=wasm go build -o arbiter.wasm "$repo_root/cmd/arbiter-wasm"

echo "copying wasm_exec.js + loader.js…"
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./wasm_exec.js
cp "$repo_root/cmd/arbiter-wasm/loader.js" ./loader.js

echo "done. serve this directory over HTTP (file:// won't load WASM):"
echo "  python3 -m http.server 8080"
