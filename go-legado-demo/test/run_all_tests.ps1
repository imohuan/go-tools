$ErrorActionPreference = 'Stop'

Set-Location -LiteralPath (Join-Path $PSScriptRoot "..")

Write-Host "[test] preparing root test files..."
$tmpFiles = @()
Get-ChildItem -LiteralPath ".\test" -Filter "*_test.go" | ForEach-Object {
  $dst = Join-Path "." $_.Name
  Copy-Item -LiteralPath $_.FullName -Destination $dst -Force
  $tmpFiles += $dst
}

Write-Host "[test] running go test ."
go test .
$code = $LASTEXITCODE

Write-Host "[test] cleaning temp test files..."
foreach ($f in $tmpFiles) {
  if (Test-Path $f) {
    Remove-Item -LiteralPath $f -Force
  }
}

if ($code -ne 0) {
  Write-Host "[test] failed with exit code $code"
  exit $code
}

Write-Host "[test] all tests passed"
exit 0
