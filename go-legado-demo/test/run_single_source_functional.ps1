param(
  [string]$ConfigPath,
  [string]$SourceName,
  [string]$Keyword
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($ConfigPath)) { $ConfigPath = 'D:/CodeX/go-legado-demo/data-all.json' }
if ([string]::IsNullOrWhiteSpace($Keyword)) { $Keyword = 'test' }

Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')

if ([string]::IsNullOrWhiteSpace($SourceName)) {
  throw 'SourceName is required, example: -SourceName "武芊漫画"'
}

Write-Host ("[single] config=" + $ConfigPath)
Write-Host ("[single] source=" + $SourceName)
Write-Host ("[single] keyword=" + $Keyword)

$env:TEST_CONFIG_PATH = $ConfigPath
$env:TEST_SOURCE_NAME = $SourceName
$env:TEST_KEYWORD = $Keyword

Write-Host '[single] preparing root test files...'
$tmpFiles = @()
$srcTest = Join-Path '.\test' 'single_source_functional_test.go'
$dstTest = '.\single_source_functional_test.go'
Copy-Item -LiteralPath $srcTest -Destination $dstTest -Force
$tmpFiles += $dstTest

try {
  Write-Host '[single] running function-chain test...'
  go test . -run TestSingleSourceFlowByFunctions -v
  $code = $LASTEXITCODE
}
finally {
  Write-Host '[single] cleaning temp test files...'
  foreach ($f in $tmpFiles) {
    if (Test-Path $f) {
      Remove-Item -LiteralPath $f -Force
    }
  }
}

if ($code -ne 0) {
  Write-Host ("[single] failed with exit code " + $code)
  exit $code
}

Write-Host '[single] passed'
exit 0
