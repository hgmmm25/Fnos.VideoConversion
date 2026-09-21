# Build Environment Configuration
# Location: D:\Fnos.VideoConversion\build-env.ps1
# Source this before building: .\build-env.ps1

<#
  Sets up the build environment for FVCC and FVCS.
  Run before any build commands to ensure consistent toolchains.
#>

# --- FVCC (NAS调度端) ---
# Go 1.25.3 for FVCC backend cross-compilation (Linux ELF)
$env:GOEXE_FVCC = "C:\Users\xx318\sdk\go1.25.3\go\bin\go.exe"

# Cross-compile target for FVCC backend
$env:GOOS_FVCC   = "linux"
$env:GOARCH_FVCC = "amd64"
$env:CGO_ENABLED_FVCC = "0"

# --- FVCS (Windows渲染端) ---
# Go toolchain: use module-level go1.24.6 via GOTOOLCHAIN=local
$env:GOTOOLCHAIN  = "local"
$env:CGO_ENABLED_FVCS = "1"

# MSYS2 MinGW-w64 for CGO (gcc/ld)
$env:MSYS2_MINGW64_BIN = "C:\msys64\mingw64\bin"
$env:CC               = "$env:MSYS2_MINGW64_BIN\gcc.exe"   # 必须显式指定，cgo 才能找到编译器
$env:PATH             = "$env:MSYS2_MINGW64_BIN;$env:PATH"
# 不要设置 CGO_CFLAGS / CGO_LDFLAGS：实测设置 CGO_LDFLAGS 会导致 runtime/cgo 编译失败

# --- Go Cache (GOCACHE) ---
# IMPORTANT: Move GOCACHE to D: to avoid C: drive space pressure
# Default GOCACHE is on C: which has only 15.2 GB free.
if (-not (Test-Path "D:\GoCache")) {
    New-Item -ItemType Directory -Path "D:\GoCache" -Force | Out-Null
}
$env:GOCACHE = "D:\GoCache"

# --- Verify Setup ---
Write-Host "[ENV] FVCC Go: $($env:GOEXE_FVCC)" -ForegroundColor Cyan
Write-Host "[ENV] FVCC target: $($env:GOOS_FVCC)/$($env:GOARCH_FVCC) CGO=$($env:CGO_ENABLED_FVCC)" -ForegroundColor Cyan
Write-Host "[ENV] FVCS GOTOOLCHAIN: $($env:GOTOOLCHAIN) CGO=$($env:CGO_ENABLED_FVCS)" -ForegroundColor Cyan
Write-Host "[ENV] GCC: $env:MSYS2_MINGW64_BIN\gcc.exe" -ForegroundColor Cyan
Write-Host "[ENV] GOCACHE: $env:GOCACHE" -ForegroundColor Cyan

if (-not (Test-Path $env:GOEXE_FVCC)) {
    Write-Host "[WARN] FVCC Go toolchain not found. Run build.ps1 instead which handles this." -ForegroundColor Yellow
}

# Verify GCC
if (-not (Test-Path "$env:MSYS2_MINGW64_BIN\gcc.exe")) {
    Write-Host "[WARN] MSYS2 mingw64 gcc not found at $env:MSYS2_MINGW64_BIN\gcc.exe" -ForegroundColor Red
    Write-Host "    FVCS requires CGO. Ensure MSYS2 is installed and mingw64\bin is on PATH." -ForegroundColor Red
}
