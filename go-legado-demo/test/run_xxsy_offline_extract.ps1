param(
  [string]$HtmlPath
)

$ErrorActionPreference = 'Stop'

if ([string]::IsNullOrWhiteSpace($HtmlPath)) {
  $HtmlPath = 'D:/CodeX/go-legado-demo/debug_logs/20260528/195647_source6_潇湘书院_chapter_raw.html'
}

Set-Location -LiteralPath (Join-Path $PSScriptRoot '..')

if (!(Test-Path -LiteralPath $HtmlPath)) {
  throw "html file not found: $HtmlPath"
}

Write-Host ("[offline] html=" + $HtmlPath)
$env:TEST_XXSY_HTML_PATH = $HtmlPath

Write-Host '[offline] preparing root test file...'
$tmpFiles = @()
$srcTest = Join-Path '.\test' 'xxsy_chapter_offline_extract_test.go'
$dstTest = '.\xxsy_chapter_offline_extract_test.go'
Copy-Item -LiteralPath $srcTest -Destination $dstTest -Force
$tmpFiles += $dstTest

try {
  Write-Host '[offline] running offline extraction test...'
  go test . -run TestXXSYChapterOfflineExtract -v
  $code = $LASTEXITCODE
}
finally {
  Write-Host '[offline] cleaning temp test file...'
  foreach ($f in $tmpFiles) {
    if (Test-Path $f) {
      Remove-Item -LiteralPath $f -Force
    }
  }
}

if ($code -ne 0) {
  Write-Host ("[offline] failed with exit code " + $code)
  exit $code
}

Write-Host '[offline] passed'
exit 0

