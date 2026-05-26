# 在项目目录执行: .\build.ps1
$ErrorActionPreference = "Stop"
$root = $PSScriptRoot
Push-Location $root
try {
    go build -ldflags="-s -w" -o flashvoice-cli.exe .
    Write-Host "已生成: $root\flashvoice-cli.exe"
} finally {
    Pop-Location
}
