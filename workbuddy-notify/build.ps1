# Build script for workbuddy-notify
# Usage: .\build.ps1

param(
    [string]$ExeName = "workbuddy-notify.exe"
)

$ErrorActionPreference = "Stop"

Write-Host "=== CodeBuddy Notify Build ===" -ForegroundColor Cyan

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) { Write-Error "Go 未安装"; exit 1 }
Write-Host "[OK] Go: $(go version)" -ForegroundColor Green

Set-Location $PSScriptRoot

$dist = "$PSScriptRoot\dist"
if (-not (Test-Path $dist)) { New-Item -ItemType Directory -Path $dist | Out-Null }

Write-Host "[1/2] 整理模块依赖..." -ForegroundColor Yellow
Push-Location app
go mod tidy
if ($LASTEXITCODE -ne 0) { Pop-Location; Write-Error "go mod tidy 失败"; exit 1 }

Write-Host "[2/2] 编译 $ExeName (CGO=0)..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w" -o "$dist\$ExeName" .
Pop-Location
if ($LASTEXITCODE -ne 0) { Write-Error "编译失败"; exit 1 }

$fileInfo = Get-Item "$dist\$ExeName"
$sizeKB = [math]::Round($fileInfo.Length / 1024, 1)
Write-Host ""
Write-Host "[OK] 构建成功" -ForegroundColor Green
Write-Host "  输出: $($fileInfo.FullName)" -ForegroundColor White
Write-Host "  大小: ${sizeKB}KB" -ForegroundColor White
Write-Host ""
Write-Host "运行: .\run.ps1        (任务监听 + Web 服务)" -ForegroundColor Yellow
Write-Host "      .\run.ps1 -server (仅 Web 服务)"         -ForegroundColor Yellow
Write-Host "      .\run.ps1 -monitor(仅任务监听)"           -ForegroundColor Yellow
