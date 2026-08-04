$ErrorActionPreference = 'Stop'
$projectRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $projectRoot
$env:PYTHONUTF8 = '1'

$goFiles = @(rg --files -g '*.go')
$unformatted = @(gofmt -l $goFiles)
if ($unformatted.Count -gt 0) {
  throw "gofmtが必要なファイルがあります: $($unformatted -join ', ')"
}

# ----------------------------------------

go test ./...
if ($LASTEXITCODE -ne 0) {
  throw 'Go単体テストに失敗しました。'
}

# ----------------------------------------

go vet ./...
if ($LASTEXITCODE -ne 0) {
  throw 'go vetに失敗しました。'
}

# ----------------------------------------

go mod tidy -diff
if ($LASTEXITCODE -ne 0) {
  throw 'go.modまたはgo.sumが整理されていません。'
}

# ----------------------------------------

python -m unittest discover -s python -p 'test_*.py'
if ($LASTEXITCODE -ne 0) {
  throw 'Python単体テストに失敗しました。'
}

# ----------------------------------------

$buildDirectory = Join-Path $projectRoot '.cache\test-build'
New-Item -ItemType Directory -Force -Path $buildDirectory | Out-Null
$buildOutput = Join-Path $buildDirectory 'MarketDataCollector.exe'
go build -o $buildOutput .
if ($LASTEXITCODE -ne 0) {
  throw 'Goビルドに失敗しました。'
}

Write-Host 'すべてのテストとビルドに成功しました。'
