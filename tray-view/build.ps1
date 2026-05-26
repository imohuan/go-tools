# Build tray-view.exe (Windows)
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

go mod tidy
go build -ldflags="-H windowsgui -s -w" -o tray-view.exe .

Write-Host "Built: $PSScriptRoot\tray-view.exe"
