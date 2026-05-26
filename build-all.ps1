# 对所有含 build.ps1 的子项目执行构建，并将 exe 复制到目标目录。
# 目标目录优先级: -BinDir 参数 > 环境变量 GO_TOOLS_BIN > .env > C:\UserApp\Bin
#
# 用法:
#   .\build-all.ps1
#   .\build-all.ps1 -BinDir D:\Tools\Bin
#   $env:GO_TOOLS_BIN = "D:\Tools\Bin"; .\build-all.ps1

param(
    [string]$BinDir
)

$ErrorActionPreference = "Stop"
$root = $PSScriptRoot

function Import-DotEnv {
    param([string]$Path)
    if (-not (Test-Path -LiteralPath $Path)) { return }
    Get-Content -LiteralPath $Path -Encoding UTF8 | ForEach-Object {
        $line = $_.Trim()
        if ($line -eq '' -or $line.StartsWith('#')) { return }
        if ($line -match '^\s*([^#=]+)\s*=\s*(.*)\s*$') {
            $name = $matches[1].Trim()
            $value = $matches[2].Trim().Trim('"').Trim("'")
            Set-Item -Path "Env:$name" -Value $value
        }
    }
}

Import-DotEnv (Join-Path $root ".env")

if (-not $BinDir) {
    $BinDir = $env:GO_TOOLS_BIN
}
if (-not $BinDir) {
    $BinDir = "C:\UserApp\Bin"
}

$BinDir = [System.IO.Path]::GetFullPath($BinDir)
New-Item -ItemType Directory -Force -Path $BinDir | Out-Null
Write-Host "目标目录: $BinDir"

$projects = Get-ChildItem -Path $root -Directory | Where-Object {
    $_.Name -ne ".git" -and (Test-Path -LiteralPath (Join-Path $_.FullName "build.ps1"))
}

if (-not $projects) {
    Write-Warning "未找到包含 build.ps1 的子目录。"
    exit 0
}

$failed = @()
foreach ($project in $projects) {
    $name = $project.Name
    $dir = $project.FullName
    Write-Host "`n=== $name ===" -ForegroundColor Cyan

    try {
        & (Join-Path $dir "build.ps1")
    } catch {
        Write-Host "构建失败: $name - $_" -ForegroundColor Red
        $failed += $name
        continue
    }

    $exes = Get-ChildItem -Path $dir -Filter "*.exe" -File
    if (-not $exes) {
        Write-Warning "$name 构建后未找到 exe。"
        $failed += $name
        continue
    }

    foreach ($exe in $exes) {
        $dest = Join-Path $BinDir $exe.Name
        Copy-Item -LiteralPath $exe.FullName -Destination $dest -Force
        Write-Host "已复制: $($exe.Name) -> $dest" -ForegroundColor Green
    }
}

Write-Host ""
if ($failed.Count -gt 0) {
    Write-Host "失败项目: $($failed -join ', ')" -ForegroundColor Red
    exit 1
}
Write-Host "全部完成。" -ForegroundColor Green
