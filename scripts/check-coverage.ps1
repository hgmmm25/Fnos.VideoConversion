# FVCC 覆盖率检查脚本（混乱报告 P2-3：覆盖率基线，当前门槛 55%，后续补测试后上调至 60%）
# 用法：powershell -File scripts/check-coverage.ps1 [-Threshold 55] [-Short] [-Race]
#   -Threshold  覆盖率阈值（默认 55，低于则退出码 1）
#   -Short      加 -short（本地快速回归；CI 不用）
#   -Race       加 -race -covermode=atomic（CI 用）
# 输出：go tool cover -func 汇总 + 总覆盖率，达标退出码 0，未达标退出码 1。

param(
    [int]$Threshold = 55,
    [switch]$Short,
    [switch]$Race
)

$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$serverDir = Join-Path (Join-Path $root 'FVCC') 'server'
$covFile = Join-Path $env:TEMP 'fvcc_coverage.out'

if (-not (Test-Path $serverDir)) { throw "server 目录不存在: $serverDir" }

Push-Location $serverDir
try {
    $goArgs = @('test')
    if ($Race) {
        $goArgs += @('-race', '-covermode=atomic')
    } elseif ($Short) {
        $goArgs += '-short'
    }
    # 覆盖口径：-coverpkg=./... 全包插桩（含 logger/smbshare 等无测试子包），
    # 单测试目标 '.' 保证 coverprofile 落盘；与 CI 全包统计口径一致。
    $goArgs += @('-coverpkg=./...', "-coverprofile=$covFile", '.')

    Write-Host "==> go $($goArgs -join ' ')"
    & go @goArgs
    if ($LASTEXITCODE -ne 0) { throw 'go test 失败，覆盖率未统计' }

    $funcOut = & go tool cover -func $covFile
    $funcOut | ForEach-Object { Write-Host $_ }
    $totalLine = $funcOut | Select-String '^total:'
    if (-not $totalLine) { throw '无法解析 go tool cover 输出中的 total 行' }

    $m = [regex]::Match($totalLine.Line, '([\d.]+)%')
    if (-not $m.Success) { throw "无法从 total 行解析百分比: $($totalLine.Line)" }
    $pct = [double]$m.Groups[1].Value

    Write-Host ("==> 总覆盖率: {0}% (阈值 {1}%)" -f $pct, $Threshold)
    if ($pct -lt $Threshold) {
        Write-Host "FAIL: 覆盖率低于阈值 $Threshold%"
        exit 1
    }
    Write-Host 'PASS: 覆盖率达标'
    exit 0
} finally {
    Pop-Location
    Remove-Item $covFile -ErrorAction SilentlyContinue
}
