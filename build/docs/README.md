# Documentation Build Scripts

This directory contains scripts used to build the WebAssembly (WASM) components for the Watchtower documentation site.

## Files

- `build-tplprev.sh` - Bash script for building the template preview WASM binary
- `build-tplprev.ps1` - PowerShell script for building the template preview WASM binary

## Purpose

These scripts build the `tplprev.wasm` WebAssembly binary and `wasm_exec.js` runtime that power the interactive template preview feature on the documentation site. The scripts:

1. Copy or download `wasm_exec.js` from the Go installation or GitHub
2. Build the WASM binary from `build/tplprev/main_wasm.go`
3. Output files to `docs/assets/` for inclusion in the documentation site

## Usage

The scripts are automatically executed by the GitHub Actions workflow `.github/workflows/publish-docs.yaml` during documentation builds. They can also be run manually:

```bash
# From repository root
./build/docs/build-tplprev.sh
```

## Dependencies

- Go 1.19+ (for WASM compilation)
- Access to `go env GOROOT` or internet connection (for downloading `wasm_exec.js`)

## Output

- `docs/assets/tplprev.wasm` - The compiled WebAssembly binary
- `docs/assets/wasm_exec.js` - Go's WASM runtime JavaScript
