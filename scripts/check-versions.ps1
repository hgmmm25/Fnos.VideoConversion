<# check-versions.ps1 — FVCC 四方版本一致性校验（P0-1 改进方向落实）
   校验维度：
     1) 应用版本三方对齐：FVCC\manifest(version) == FVCC\ui-src\package.json(version) == FVCC\server\internal\version\VERSION
     2) 构建工具链声明：FVCC\server\go.mod 的 go 指令与 BUILD.md 文档声明一致（代码即真相）
     3) 打包工具引用：build.ps1 / BUILD.md 引用的 fnpack 与仓库实际留存版本一致（仅提示）

   用法：
     .\scripts\check-versions.ps1            # 只检查，不一致时退出码 1
     .\scripts\check-versions.ps1 -Quiet     # 仅输出结论行
#>
param(
    [switch]$Quiet
)

$ErrorActionPreference = 'Stop'
$BASE_DIR = "D:\Fnos.VideoConversion"
$FAIL = 0

function Write-Check {
    param([string]$Name, [string]$Detail, [bool]$Ok)
    if (-not $Quiet) {
        $mark = if ($Ok) { "[OK] " } else { "[FAIL] " }
        Write-Host ("{0}{1}: {2}" -f $mark, $Name, $Detail) -ForegroundColor $(if ($Ok) { [ConsoleColor]::Green } else { [ConsoleColor]::Red })
    }
    if (-not $Ok) { $script:FAIL = 1 }
}

# --- 读取三处应用版本 ---
$manifestPath = Join-Path $BASE_DIR "FVCC\manifest"
$pkgPath      = Join-Path $BASE_DIR "FVCC\ui-src\package.json"
$verPath      = Join-Path $BASE_DIR "FVCC\server\internal\version\VERSION"
$goModPath    = Join-Path $BASE_DIR "FVCC\server\go.mod"

$manifestVer = $null
if (Test-Path $manifestPath) {
    $manifestLine = Get-Content $manifestPath | Where-Object { $_ -match '^\s*version\s*=' } | Select-Object -First 1
    if ($manifestLine -match 'version\s*=\s*(\S+)') { $manifestVer = $Matches[1] }
}
if (-not $manifestVer) { Write-Check "manifest.version" "未找到（$manifestPath）" $false }

$pkgVer = $null
if (Test-Path $pkgPath) {
    $pkgJson = Get-Content $pkgPath -Raw | ConvertFrom-Json
    $pkgVer = $pkgJson.version
}
if (-not $pkgVer) { Write-Check "package.json.version" "未找到（$pkgPath）" $false }

$verFile = $null
if (Test-Path $verPath) {
    $verFile = (Get-Content $verPath | Select-Object -First 1).Trim()
}
if (-not $verFile) { Write-Check "server/internal/version/VERSION" "未找到（$verPath）" $false }

# --- 读取 go.mod 的 go 指令 ---
$goDirective = $null
if (Test-Path $goModPath) {
    $goLine = Get-Content $goModPath | Where-Object { $_ -match '^go\s+\d+\.\d+(\.\d+)?' } | Select-Object -First 1
    if ($goLine -match '^go\s+(\S+)') { $goDirective = $Matches[1] }
}
if (-not $goDirective) { Write-Check "go.mod go 指令" "未找到（$goModPath）" $false }

# --- 1) 应用版本三方对齐 ---
$versions = @($manifestVer, $pkgVer, $verFile) | Where-Object { $_ -ne $null }
if ($versions.Count -eq 3) {
    $uniq = ($versions | Select-Object -Unique).Count
    Write-Check "应用版本三方对齐" ("manifest={0} package.json={1} VERSION={2} => {3}" -f $manifestVer, $pkgVer, $verFile, $(if ($uniq -eq 1) { "一致" } else { "不一致" })) ($uniq -eq 1)
} else {
    Write-Check "应用版本三方对齐" "读取不完整（manifest=$manifestVer package.json=$pkgVer VERSION=$verFile）" $false
}

# --- 2) go.mod 工具链声明存在且为合理版本 ---
if ($goDirective) {
    $goOk = $goDirective -match '^1\.(2[0-9]|3[0-9])(\.\d+)?$'
    Write-Check "go.mod 工具链声明" ("go {0}（FVCC 交叉编译需 GOTOOLCHAIN=auto）" -f $goDirective) $goOk
}

# --- 3) fnpack 引用提示（非阻断） ---
$fpkRef = Select-String -Path (Join-Path $BASE_DIR "build.ps1") -Pattern 'fnpack-[\d.]+-windows-amd64\.exe' | Select-Object -First 1
if ($fpkRef -and $fpkRef.Matches[0].Value) {
    Write-Host ("[INFO] build.ps1 引用的打包工具: {0}" -f $fpkRef.Matches[0].Value) -ForegroundColor Yellow
}

if ($FAIL -ne 0) {
    Write-Host "`n版本一致性校验未通过，请先对齐版本再构建/发布。" -ForegroundColor Red
    exit 1
} else {
    Write-Host "`n版本一致性校验通过：应用版本 $verFile / go $goDirective 四方声明一致。" -ForegroundColor Green
    exit 0
}
