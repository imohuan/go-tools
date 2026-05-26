# 构建 kills.exe（无控制台窗口）
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not (Test-Path "assets\icon.ico")) {
    New-Item -ItemType Directory -Force -Path "assets" | Out-Null
    Add-Type -AssemblyName System.Drawing
    $bmp = New-Object System.Drawing.Bitmap 16, 16
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.Clear([System.Drawing.Color]::FromArgb(231, 76, 60))
    $g.Dispose()
    $icon = [System.Drawing.Icon]::FromHandle($bmp.GetHicon())
    $path = Join-Path $PWD "assets\icon.ico"
    $fs = [IO.File]::Create($path)
    $icon.Save($fs)
    $fs.Close()
    $bmp.Dispose()
}

go mod tidy
go build -ldflags="-H windowsgui -s -w" -o kills.exe .
Write-Host "已生成: $PWD\kills.exe"
