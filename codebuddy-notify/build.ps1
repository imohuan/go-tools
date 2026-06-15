# Build script for codebuddy-notify
# Usage: .\build.ps1 [-ExeName codebuddy-notify.exe]

param(
    [string]$ExeName = "codebuddy-notify.exe"
)

$ErrorActionPreference = "Stop"

Write-Host "=== CodeBuddy Notify Build ===" -ForegroundColor Cyan

$go = Get-Command go -ErrorAction SilentlyContinue
if (-not $go) {
    Write-Error "Go 未安装"
    exit 1
}
Write-Host "[OK] Go version: $(go version)" -ForegroundColor Green

Set-Location $PSScriptRoot

Write-Host "[1/2] 整理模块依赖..." -ForegroundColor Yellow
go mod tidy
if ($LASTEXITCODE -ne 0) { Write-Error "go mod tidy 失败"; exit 1 }

Write-Host "[2/2] 编译 $ExeName (CGO=0)..." -ForegroundColor Yellow
$env:CGO_ENABLED = "0"
go build -ldflags="-s -w" -o $ExeName .
if ($LASTEXITCODE -ne 0) { Write-Error "编译失败"; exit 1 }

$fileInfo = Get-Item $ExeName
$sizeKB = [math]::Round($fileInfo.Length / 1024, 1)
Write-Host ""
Write-Host "[OK] 构建成功" -ForegroundColor Green
Write-Host "  输出文件: $($fileInfo.FullName)" -ForegroundColor White
Write-Host "  文件大小: ${sizeKB}KB" -ForegroundColor White
Write-Host ""
Write-Host "运行方式:" -ForegroundColor Cyan
Write-Host "  monitor: .\run.ps1                 (监听任务状态)" -ForegroundColor Yellow
Write-Host "  server:  .\run.ps1 -server         (Web API 对话查看器)" -ForegroundColor Yellow
