#!/bin/bash

# Navigate to the repository root
cd $(git rev-parse --show-toplevel)

# Copy wasm_exec.js from GOROOT/lib/wasm to docs/assets/
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" ./docs/assets/

# Build the tplprev WebAssembly binary
GOARCH=wasm GOOS=js go build -o ./docs/assets/tplprev.wasm ./tools/tplprev
