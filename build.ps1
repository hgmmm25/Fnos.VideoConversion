<# Build script for FVCC + FVCS — single-command out-the-door.
   Run in Windows PowerShell (any version).
   Path: D:\Fnos.VideoConversion\build.ps1

   Usage:
     .\build.ps1                          # FVCC + FVCS (default)
     .\build.ps1 -Target FVCS             # Only FVCS
     .\build.ps1 -Target FVCC             # Only FVCC
     .\build.ps1 -Target FVCS -Clean      # Clean then build FVCS
     .\build.ps1 -Target FVCS -NoTest     # Build without tests
#>
param(
    [ValidateSet('FVCC', 'FVCS', 'ALL')]
    [string]$Target = 'ALL',
    [switch]$Clean,
    [switch]$NoTest
)

# Track overall start time for summary
$script:BUILD_START = Get-Date

# --- Configuration ---
$BASE_DIR = "D:\Fnos.VideoConversion"
$FVCC_DIR = "$BASE_DIR\FVCC"
$FVCS_DIR = "$BASE_DIR\FVCS"
$APP_DIR  = "$FVCC_DIR\app"          # FVCC app bundle directory

# Go toolchain — pick from known SDK locations
$SDK_PATHS = @(
    "C:\Users\xx318\sdk\go1.25.3\go\bin\go.exe",
    "C:\Users\xx318\sdk\go1.24.6\go\bin\go.exe",
    "C:\Program Files\Go\bin\go.exe",
    "go"
)
$GO_EXE = $null
foreach ($p in $SDK_PATHS) {
    if ($p -eq 'go') {
        $cmd = Get-Command go -ErrorAction SilentlyContinue
        if ($cmd) { $GO_EXE = $cmd.Source; break }
    } elseif (Test-Path $p) {
        $GO_EXE = $p; break
    }
}
if (-not $GO_EXE) {
    Write-Host "ERROR: No Go installation found. Check build-env.ps1 for paths." -ForegroundColor Red
    exit 1
}

# Use GOTOOLCHAIN=auto so the go command picks the toolchain required by go.mod.
# Both FVCC (server/go.mod) and FVCS (go.mod) declare `go 1.27.1`; the go1.27.1
# toolchain is already cached in GOMODCACHE (golang.org/toolchain@v0.0.1-go1.27.1),
# so auto incurs no download and avoids "go.mod requires go >= 1.27.1" failures.
$GOTOOLCHAIN = "auto"

# FVCC backend cross-compile target
$FVCC_GOOS    = "linux"
$FVCC_GOARCH  = "amd64"
$FVCC_CGO     = "0"

# FVCS build: Windows native, CGO enabled
$FVCS_CGO = "1"

# MSYS2 MinGW-w64
$MINGW_BIN = "C:\msys64\mingw64\bin"
$MINGW_GCC = "$MINGW_BIN\gcc.exe"

# Output paths
$FVCS_BINARY  = "$FVCS_DIR\fvcs-service.exe"
$FVCS_OUTPUT  = "$FVCC_DIR\fvcs-service.exe"  # copy here for fnpack
$GOCACHE_DIR  = "D:\GoCache"

# fnpack (fnOS fpk packer). Switched from 1.2.1 (FVCC\fnpack-1.2.1.exe) to 1.2.3.
$FNPACK_EXE   = "D:\Fnos.VideoConversion\fnpack-1.2.3-windows-amd64.exe"

# Colors
$ErrorActionPreference = 'Stop'
$COLOR_RED    = [ConsoleColor]::Red
$COLOR_GREEN  = [ConsoleColor]::Green
$COLOR_YELLOW = [ConsoleColor]::Yellow
$COLOR_CYAN   = [ConsoleColor]::Cyan

function Write-Step {
    param([string]$Msg)
    Write-Host "`n===> $Msg" -ForegroundColor $COLOR_CYAN
}

function Write-Ok {
    param([string]$Msg)
    Write-Host "[OK] $Msg" -ForegroundColor $COLOR_GREEN
}

function Write-Err {
    param([string]$Msg)
    Write-Host "[FAIL] $Msg" -ForegroundColor $COLOR_RED
}

# --- Pre-flight checks ---
Write-Step "Pre-flight checks"

# Verify Go
$goVer = (& $GO_EXE version 2>&1)
Write-Ok "Go: $GO_EXE ($goVer)"

# Set GOTOOLCHAIN
$env:GOTOOLCHAIN = $GOTOOLCHAIN
Write-Ok "GOTOOLCHAIN: $GOTOOLCHAIN"

# Verify MSYS2 MinGW-w64 GCC for CGO
if (-not (Test-Path $MINGW_GCC)) {
    Write-Err "MSYS2 mingw64 gcc not found at: $MINGW_GCC"
    Write-Host "    FVCS requires CGO. Please ensure C:\msys64\mingw64\bin is on PATH."
    exit 1
}
Write-Ok "MSYS2 GCC: $MINGW_GCC"

# Add MSYS2 bin to PATH for CGO
$env:PATH = "$MINGW_BIN;$env:PATH"
# Explicitly point cgo at the MSYS2 gcc (required for Go cgo to find compiler)
$env:CC = $MINGW_GCC

# Verify Node.js (for FVCC frontend)
$NODE_EXISTS = Get-Command node -ErrorAction SilentlyContinue
$NPM_EXISTS  = Get-Command npm -ErrorAction SilentlyContinue
if (-not $NODE_EXISTS -or -not $NPM_EXISTS) {
    Write-Err "Node.js or npm not found. FVCC frontend requires Node.js."
    exit 1
}
Write-Ok "Node.js: $(node --version)"

# Set GOCACHE to D:
if (-not (Test-Path $GOCACHE_DIR)) {
    New-Item -ItemType Directory -Path $GOCACHE_DIR -Force | Out-Null
}
$env:GOCACHE = $GOCACHE_DIR
Write-Ok "GOCACHE: $env:GOCACHE"

# --- Clean ---
if ($Clean) {
    Write-Step "Clean"
    if ($Target -eq 'FVCC' -or $Target -eq 'ALL') {
        Remove-Item "$APP_DIR\fvcc" -Force -ErrorAction SilentlyContinue
        Remove-Item "$APP_DIR\ui" -Recurse -Force -ErrorAction SilentlyContinue
        Remove-Item "$FVCC_DIR\fvcs-service.exe" -Force -ErrorAction SilentlyContinue
        Write-Ok "FVCC app/ cleaned"
    }
    if ($Target -eq 'FVCS' -or $Target -eq 'ALL') {
        Remove-Item $FVCS_BINARY -Force -ErrorAction SilentlyContinue
        Write-Ok "FVCS cleaned"
    }
}

# --- Build FVCC ---
if ($Target -eq 'FVCC' -or $Target -eq 'ALL') {
    Write-Step "Building FVCC"
    $fvccStart = Get-Date

    # Step 1+2: FVCC frontend (npm) and backend (go cross-compile) run in parallel.
    # They are independent (vite output -> app/ui, go build -> app/fvcc) and this
    # hides the slower side, saving roughly min(frontend, backend) wall-clock time.
    Write-Host "  [1/2] Building FVCC frontend + backend (parallel)..."
    $frontendJob = Start-Job -ScriptBlock {
        param($uiDir)
        Push-Location $uiDir
        npm run build
        $code = $LASTEXITCODE
        Pop-Location
        # PS 5.1 jobs do not reliably surface `exit` codes via ChildJobs[0].ExitCode,
        # so surface failure via a thrown error -> job State 'Failed'.
        if ($code -ne 0) { throw "npm build failed with exit code $code" }
    } -ArgumentList "$FVCC_DIR\ui-src"

    # Backend in the foreground (cross-compile Linux ELF)
    $fvccBackendStart = Get-Date
    $env:CGO_ENABLED = $FVCC_CGO
    $env:GOOS = $FVCC_GOOS
    $env:GOARCH = $FVCC_GOARCH
    Push-Location $FVCC_DIR\server
    # NOTE: flags must precede the package path ("."), and we must build from inside
    # the module dir, otherwise `go build` fails with "cannot find main module" /
    # "malformed import path" when invoked from the repo root.
    & $GO_EXE build -trimpath -ldflags "-s -w" -o "$APP_DIR\fvcc" .
    $fvccBackendStatus = $LASTEXITCODE
    Pop-Location
    $env:GOOS = ""
    $env:GOARCH = ""
    $env:CGO_ENABLED = ""
    if ($fvccBackendStatus -ne 0) {
        Write-Err "FVCC backend build failed"
        exit 1
    }
    $backendDuration = ((Get-Date) - $fvccBackendStart).TotalSeconds
    Write-Ok "FVCC backend built in $([math]::Round($backendDuration, 1))s"

    # Collect the parallel frontend job result
    Wait-Job $frontendJob | Out-Null
    Receive-Job $frontendJob | Out-String | Write-Host
    $npmStatus = if ($frontendJob.State -eq 'Failed') { 1 } else { 0 }
    Remove-Job $frontendJob -Force
    if ($npmStatus -ne 0) {
        Write-Err "FVCC frontend build failed (npm exit code $npmStatus)"
        exit 1
    }
    $frontendDuration = ((Get-Date) - $fvccStart).TotalSeconds
    Write-Ok "FVCC frontend built in $([math]::Round($frontendDuration, 1))s (parallel with backend)"

    # Step 3: FVCC package (fnpack)
    # fnpack recursively scans its working directory during the "Verifying files"
    # phase, so running it inside the real FVCC dir (temp/, logs/, history .fpk,
    # fvcc.exe etc. = thousands of files) costs ~14s vs ~2s in a clean dir.
    # Pack in a clean staging dir, then copy the resulting fpk back.
    Write-Host "  [2/2] Creating FVCC fpk..."
    $fvccFpStart = Get-Date
    $stageDir = "$FVCC_DIR\temp\fpk_stage"
    if (Test-Path $stageDir) { Remove-Item $stageDir -Recurse -Force }
    New-Item -ItemType Directory -Path $stageDir | Out-Null
    Copy-Item "$FVCC_DIR\app" "$stageDir\app" -Recurse -Force
    Copy-Item "$FVCC_DIR\cmd" "$stageDir\cmd" -Recurse -Force
    Copy-Item "$FVCC_DIR\config" "$stageDir\config" -Recurse -Force
    Copy-Item "$FVCC_DIR\manifest" "$stageDir\manifest" -Force
    Copy-Item "$FVCC_DIR\ICON.PNG" "$stageDir\ICON.PNG" -Force
    Copy-Item "$FVCC_DIR\ICON_256.PNG" "$stageDir\ICON_256.PNG" -Force
    Copy-Item $FNPACK_EXE "$stageDir\fnpack.exe" -Force
    Push-Location $stageDir
    $fpOutput = & .\fnpack.exe build 2>&1 | Out-String
    $fpOutput | Write-Host
    $fpStatus = $LASTEXITCODE
    Pop-Location
    # fnpack returns exit code 0 even when "Packing failed" is printed, so the
    # success criterion must be the output text, not the exit code.
    if ($fpStatus -ne 0 -or $fpOutput -notmatch "Packing successfully") {
        Write-Err "FVCC fnpack failed"
        exit 1
    }
    Copy-Item "$stageDir\fvcc.fpk" "$FVCC_DIR\fvcc.fpk" -Force
    $fpDuration = ((Get-Date) - $fvccFpStart).TotalSeconds
    Write-Ok "FVCC fpk created: fvcc.fpk in $([math]::Round($fpDuration, 1))s"
}

# --- Build FVCS ---
if ($Target -eq 'FVCS' -or $Target -eq 'ALL') {
    Write-Step "Building FVCS"
    $fvcsStart = Get-Date

    # Step 1: FVCS test (if not NoTest)
    if (-not $NoTest) {
        Write-Host "  [1/2] Running FVCS tests..."
        Push-Location $FVCS_DIR
        # Only test pkg and cmd/service, NOT cmd/ui (requires CGO fyne)
        $env:CGO_ENABLED = $FVCS_CGO
        go test ./pkg/... ./cmd/service -count=1 -v
        $testStatus = $LASTEXITCODE
        $env:CGO_ENABLED = ""
        Pop-Location
        if ($testStatus -ne 0) {
            Write-Err "FVCS tests failed (exit code $testStatus)"
            Write-Host "    Skipping build. Fix tests before proceeding."
            exit 1
        }
        Write-Ok "FVCS tests passed"
    }

    # Step 2: FVCS build
    Write-Host "  [2/2] Building FVCS service..."
    $fvcsBuildStart = Get-Date
    Push-Location $FVCS_DIR
    $env:CGO_ENABLED = $FVCS_CGO
    & $GO_EXE build -trimpath -ldflags "-s -w" -o "$FVCS_BINARY" .\cmd\service
    $buildStatus = $LASTEXITCODE
    $env:CGO_ENABLED = ""
    Pop-Location
    if ($buildStatus -ne 0) {
        Write-Err "FVCS build failed (exit code $buildStatus)"
        exit 1
    }
    $fvcsBuildDuration = ((Get-Date) - $fvcsBuildStart).TotalSeconds
    Write-Ok "FVCS service built in $([math]::Round($fvcsBuildDuration, 1))s: $FVCS_BINARY"

    # Copy to FVCC directory (kept as a convenience artifact for the fnOS app source tree)
    Copy-Item $FVCS_BINARY "$FVCC_DIR\fvcs-service.exe" -Force
    Write-Ok "Copied to $FVCC_DIR\fvcs-service.exe"

    # NOTE: fpk packaging is intentionally NOT repeated here. The FVCC block above
    # already produced the fpk in ALL mode; the FVCS binary does not enter app.tgz
    # (only app/fvcc + app/ui do), so a second `fnpack build` was pure redundant work.
    # Run `.\build.ps1 -Target FVCC` (or ALL) to (re)create the fpk.
}

# --- Summary ---
$totalDuration = ((Get-Date) - $script:BUILD_START).TotalSeconds
Write-Host "`n"
Write-Host "=== BUILD COMPLETE ===" -ForegroundColor $COLOR_GREEN
Write-Host "Total time: $([math]::Round($totalDuration, 1))s" -ForegroundColor $COLOR_YELLOW
