$ErrorActionPreference = 'Stop'

Set-Location -LiteralPath (Join-Path $PSScriptRoot "..")

Write-Host "[rule-test] preparing main_rule_support_test.go ..."
$src = Join-Path ".\test" "main_rule_support_test.go"
$dst = Join-Path "." "main_rule_support_test.go"
Copy-Item -LiteralPath $src -Destination $dst -Force

try {
  Write-Host "[rule-test] running go test . -run RuleSupport -v"
  go test . -run RuleSupport -v
  $code = $LASTEXITCODE
}
finally {
  Write-Host "[rule-test] cleaning temp file ..."
  if (Test-Path $dst) {
    Remove-Item -LiteralPath $dst -Force
  }
}

if ($code -ne 0) {
  Write-Host "[rule-test] failed with exit code $code"
  exit $code
}

Write-Host "[rule-test] all rule-support tests passed"
exit 0
