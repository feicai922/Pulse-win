$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

Write-Host "[1/4] Stopping running Pulse instances..."
Get-Process -Name Pulse, PulseSetup -ErrorAction SilentlyContinue | Stop-Process -Force

Write-Host "[2/4] Generating multi-resolution icon & Windows PE resources..."
go run ./cmd/genrsrc
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "[3/4] Building Pulse.exe..."
go build -ldflags "-H windowsgui -s -w" -o Pulse.exe .
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "[4/4] Packaging PulseSetup.exe..."
$payloadDir = Join-Path $PSScriptRoot "cmd\installer\payload"
New-Item -ItemType Directory -Force -Path $payloadDir | Out-Null
$gzPath = Join-Path $payloadDir "Pulse.exe.gz"

$inStream = [System.IO.File]::OpenRead((Join-Path $PSScriptRoot "Pulse.exe"))
$outStream = [System.IO.File]::Create($gzPath)
$gzip = New-Object System.IO.Compression.GzipStream($outStream, [System.IO.Compression.CompressionLevel]::Optimal)
$inStream.CopyTo($gzip)
$gzip.Dispose()
$outStream.Dispose()
$inStream.Dispose()

go build -tags installer -ldflags "-H windowsgui -s -w" -o PulseSetup.exe ./cmd/installer
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

Write-Host "Build complete:"
Get-Item Pulse.exe, PulseSetup.exe | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
