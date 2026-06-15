# Run script for workbuddy-notify
# Usage:
#   .\run.ps1                        - 任务监听 + Web 服务（默认 8080）
#   .\run.ps1 -Server                - 仅 Web 服务
#   .\run.ps1 -Monitor               - 仅任务监听
#   .\run.ps1 -Port 3000             - 指定端口

param(
    [switch]$Server,
    [switch]$Monitor,
    [int]$Port = 8080
)

$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

$binary = "dist\workbuddy-notify.exe"
if (-not (Test-Path $binary)) {
    Write-Error "未找到 $binary，请先运行 build.ps1"
    exit 1
}

if ($Server) {
    Write-Host "=== 启动 Web 服务 ===" -ForegroundColor Cyan
    Write-Host "地址: http://localhost:$Port" -ForegroundColor Green
    & ".\$binary" -mode server -port $Port
} elseif ($Monitor) {
    Write-Host "=== 启动任务监听 ===" -ForegroundColor Cyan
    & ".\$binary" -mode monitor
} else {
    Write-Host "=== 任务监听 + Web 服务 ===" -ForegroundColor Cyan
    Write-Host "Web:  http://localhost:$Port" -ForegroundColor Green
    Write-Host "监听: 每 5s 轮询 sessions" -ForegroundColor Yellow
    Write-Host ""
    & ".\$binary" -mode all -port $Port
}
