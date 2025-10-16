#!/bin/bash
# Builds the WebAssembly binary (tplprev.wasm) for the Watchtower template preview
# and copies wasm_exec.js to ./docs/assets/. If wasm_exec.js is not found in
# $GOROOT/lib/wasm/, it is downloaded from the Go repository.

# Exit on any error
set -e

# Navigate to the repository root
cd "$(git rev-parse --show-toplevel)"

# Create docs/assets directory if it doesn't exist
mkdir -p ./docs/assets

# Copy wasm_exec.js from GOROOT/lib/wasm or download it
echo "Copying wasm_exec.js..."
WASM_EXEC_SRC="$(go env GOROOT)/lib/wasm/wasm_exec.js"
WASM_EXEC_DEST="./docs/assets/wasm_exec.js"
WASM_EXEC_URL="https://raw.githubusercontent.com/golang/go/master/lib/wasm/wasm_exec.js"

if [ -f "$WASM_EXEC_SRC" ]; then
    cp "$WASM_EXEC_SRC" "$WASM_EXEC_DEST"
    echo "Copied wasm_exec.js to ./docs/assets/"
else
    echo "wasm_exec.js not found at $WASM_EXEC_SRC. Downloading from $WASM_EXEC_URL..."
    if ! curl -L -o "$WASM_EXEC_DEST" "$WASM_EXEC_URL"; then
        echo "Error: Failed to download wasm_exec.js from $WASM_EXEC_URL" >&2
        exit 1
    fi
    echo "Downloaded wasm_exec.js to ./docs/assets/"
fi

# Build WASM binary
echo "Building tplprev.wasm..."
GOARCH=wasm GOOS=js go build -o ./docs/assets/tplprev.wasm ./build/tplprev

# Verify output
echo "Files in ./docs/assets:"
ls -l ./docs/assets
