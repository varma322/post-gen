# Build script for postgen: builds React UI, then Go backend, producing standalone binary.
# Usage: .\build.ps1 [--output <path>] [--version <version>]

param(
    [string]$Output = "dist",
    [string]$Version = "v2.5.5"
)

# Ensure we're in the project root
if (-not (Test-Path "postgen-ui")) {
    Write-Error "postgen-ui directory not found. Run this script from the project root."
    exit 1
}

Write-Host "=== PostGen Build Script ===" -ForegroundColor Green
Write-Host "Output directory: $Output"
Write-Host "Version: $Version"
Write-Host ""

# Step 1: Build React UI
Write-Host "Step 1: Building React UI..." -ForegroundColor Cyan
Push-Location postgen-ui
try {
    $npmInstallResult = npm install
    if ($LASTEXITCODE -ne 0) {
        Write-Error "npm install failed"
        exit 1
    }

    $npmBuildResult = npm run build
    if ($LASTEXITCODE -ne 0) {
        Write-Error "npm run build failed"
        exit 1
    }
    Write-Host "✓ React UI built successfully" -ForegroundColor Green
} finally {
    Pop-Location
}

# Step 2: Verify Go dependencies
Write-Host ""
Write-Host "Step 2: Verifying Go dependencies..." -ForegroundColor Cyan
go mod download
if ($LASTEXITCODE -ne 0) {
    Write-Error "go mod download failed"
    exit 1
}
Write-Host "✓ Dependencies verified" -ForegroundColor Green

# Step 3: Build Go backend
Write-Host ""
Write-Host "Step 3: Building Go backend..." -ForegroundColor Cyan

# Create output directory if it doesn't exist
if (-not (Test-Path $Output)) {
    New-Item -ItemType Directory -Path $Output -Force | Out-Null
}

$Binary = "$Output\postgen-api_$Version.exe"
$GoCmd = "go", "build", "-o", $Binary, ".\cmd\api"
& $GoCmd

if ($LASTEXITCODE -ne 0) {
    Write-Error "go build failed"
    exit 1
}

# Verify binary was created
if (-not (Test-Path $Binary)) {
    Write-Error "Binary was not created at $Binary"
    exit 1
}

Write-Host "✓ Backend binary built successfully" -ForegroundColor Green

# Step 4: Summary
Write-Host ""
Write-Host "=== Build Complete ===" -ForegroundColor Green
Write-Host "Binary location: $Binary"
Write-Host ""
Write-Host "To run the server:"
Write-Host "  .\$Binary --addr :8088"
Write-Host ""
Write-Host "Then navigate to http://localhost:8088 in your browser."
