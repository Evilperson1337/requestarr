$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
$Patch = Join-Path $Root ".requestarr-update/requestarr-update.patch"

if (-not (Test-Path (Join-Path $Root "go.mod")) -or -not (Test-Path (Join-Path $Root "internal"))) {
    throw "Extract this archive into the Requestarr repository root, then run this script there."
}
if (-not (Get-Command git -ErrorAction SilentlyContinue)) {
    throw "git is required for merge-safe application; use .requestarr-update/manual-merge if necessary."
}
Push-Location $Root
try {
    git apply --check $Patch
    if ($LASTEXITCODE -ne 0) {
        throw "Patch check failed. No project source was modified. Use .requestarr-update/manual-merge for manual integration."
    }
    git apply $Patch
    if ($LASTEXITCODE -ne 0) { throw "git apply failed unexpectedly." }
    Write-Host "Requestarr cumulative audiobook + Chaptarr update applied successfully."
    Write-Host "Review with: git diff --stat; git diff"
    Write-Host "Then follow APPLY_UPDATE.md and run the project's normal checks."
}
finally {
    Pop-Location
}
