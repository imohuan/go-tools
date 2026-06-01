$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

Write-Host '检查端口占用: 8080'
$conns = Get-NetTCPConnection -LocalPort 8080 -ErrorAction SilentlyContinue
if ($conns) {
  $pids = $conns | Select-Object -ExpandProperty OwningProcess -Unique
  foreach ($procId in $pids) {
    try {
      Stop-Process -Id $procId -Force -ErrorAction Stop
      Write-Host "已结束占用进程 PID: $procId"
    } catch {
      Write-Host "结束 PID $procId 失败: $($_.Exception.Message)"
    }
  }
  Start-Sleep -Milliseconds 300
} else {
  Write-Host '端口未被占用'
}

Write-Host '正在启动 Go 服务...'
Write-Host '访问: http://localhost:8080'
Write-Host '按 Ctrl+C 停止服务'

go run .

