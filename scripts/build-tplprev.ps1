# build-tplprev.ps1
# Navigate to repository root
$repoRoot = git rev-parse --show-toplevel
Set-Location -Path $repoRoot

# Create docs/assets directory
New-Item -ItemType Directory -Path "./docs/assets" -Force

# Copy wasm_exec.js from GOROOT/lib/wasm
Write-Output "Copying wasm_exec.js..."
$goRoot = go env GOROOT
$wasmExecPath = "$goRoot/lib/wasm/wasm_exec.js"
if (Test-Path $wasmExecPath) {
    Copy-Item -Path $wasmExecPath -Destination "./docs/assets/wasm_exec.js" -Force
    Write-Output "Copied wasm_exec.js to ./docs/assets/"
}
else {
    Write-Output "wasm_exec.js not found at $wasmExecPath. Attempting to download..."
    $wasmExecUrl = "https://raw.githubusercontent.com/golang/go/master/lib/wasm/wasm_exec.js"
    try {
        Invoke-WebRequest -Uri $wasmExecUrl -OutFile "./docs/assets/wasm_exec.js" -ErrorAction Stop
        Write-Output "Downloaded wasm_exec.js from $wasmExecUrl"
    }
    catch {
        Write-Error "Failed to download wasm_exec.js from $wasmExecUrl. Please manually download it."
        exit 1
    }
}

# Build WASM binary
Write-Output "Building tplprev.wasm..."
$env:GOARCH = "wasm"
$env:GOOS = "js"
go build -o ./docs/assets/tplprev.wasm ./tools/tplprev

# Verify output
Write-Output "Files in ./docs/assets:"
Get-ChildItem -Path "./docs/assets"
