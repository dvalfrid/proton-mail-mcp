# PostToolUse (Edit/Write): gofmt the .go file that was just changed, so
# formatting never piles up until commit time (lefthook only checks).
$ErrorActionPreference = 'Stop'

try {
    $file = ([Console]::In.ReadToEnd() | ConvertFrom-Json).tool_input.file_path
} catch {
    exit 0
}
if (-not $file -or -not $file.EndsWith('.go')) { exit 0 }

$gofmt = Get-Command gofmt -ErrorAction SilentlyContinue
$gofmtPath = if ($gofmt) { $gofmt.Source } else { 'C:\Program Files\Go\bin\gofmt.exe' }
if (-not (Test-Path $gofmtPath)) { exit 0 }

$err = & $gofmtPath -w $file 2>&1
if ($LASTEXITCODE -ne 0) {
    # A syntax error mid-edit is normal; say so but don't block.
    [Console]::Error.WriteLine("gofmt skipped ${file}: $($err | Select-Object -First 1)")
}
exit 0
