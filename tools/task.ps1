#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Project task runner: the same commands locally and in CI.

.EXAMPLE
    ./tools/task.ps1 verify
    ./tools/task.ps1 test -Race
    ./tools/task.ps1 build -Version 0.2.0
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('fmt', 'fmt-check', 'lint', 'test', 'vuln', 'tidy-check', 'verify', 'build', 'setup')]
    [string]$Task = 'verify',

    # Run tests with the race detector (needs cgo and a C compiler; CI on Linux has both).
    [switch]$Race,

    # Version embedded in the binary by `build` (main.version).
    [string]$Version = 'dev',

    # Target for `build`, e.g. -GOOS linux -GOARCH arm64. Defaults to the host.
    [string]$GOOS = '',
    [string]$GOARCH = '',

    # Output path for `build`.
    [string]$Out = ''
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

# Pinned tools live in tools/go.mod. The argument is quoted on purpose:
# PowerShell splits an unquoted `-modfile=tools/go.mod` at the dot.
$toolsMod = '-modfile=tools/go.mod'

function Invoke-Native {
    param([string]$Exe, [string[]]$Arguments)
    Write-Host "> $Exe $($Arguments -join ' ')" -ForegroundColor DarkGray
    & $Exe @Arguments
    if ($LASTEXITCODE -ne 0) { throw "'$Exe $($Arguments -join ' ')' failed with exit code $LASTEXITCODE" }
}

function Invoke-Tool {
    param([string]$Name, [string[]]$Arguments)
    Invoke-Native 'go' (@('tool', $toolsMod, $Name) + $Arguments)
}

function Test-Tidy {
    # `go mod tidy -diff` prints the change and fails instead of writing it.
    Invoke-Native 'go' @('mod', 'tidy', '-diff')
    Push-Location tools
    try { Invoke-Native 'go' @('mod', 'tidy', '-diff') } finally { Pop-Location }
}

function Invoke-Test {
    $testArgs = @('test', '-count=1', '-coverprofile=coverage.out')
    if ($Race) { $testArgs += '-race' }
    Invoke-Native 'go' ($testArgs + @('./...'))
}

function Invoke-Build {
    $exe = if (($GOOS -eq '' -and $IsWindows) -or $GOOS -eq 'windows') { '.exe' } else { '' }
    $output = if ($Out) { $Out } else { "bin/proton-mail-mcp$exe" }
    $env:CGO_ENABLED = '0'
    if ($GOOS) { $env:GOOS = $GOOS }
    if ($GOARCH) { $env:GOARCH = $GOARCH }
    try {
        Invoke-Native 'go' @('build', '-trimpath', "-ldflags=-s -w -X main.version=$Version", '-o', $output, './cmd/proton-mail-mcp')
    } finally {
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
    }
    Write-Host "Built $output ($Version)"
}

switch ($Task) {
    'fmt'        { Invoke-Tool 'golangci-lint' @('fmt') }
    'fmt-check'  { Invoke-Tool 'golangci-lint' @('fmt', '--diff') }
    'lint'       { Invoke-Tool 'golangci-lint' @('run') }
    'test'       { Invoke-Test }
    'vuln'       { Invoke-Tool 'govulncheck' @('./...') }
    'tidy-check' { Test-Tidy }
    'build'      { Invoke-Build }
    'setup' {
        # Build lefthook to a stable path first: the git hooks it installs
        # remember the binary's location, and `go tool` runs it from the
        # build cache, which can be cleaned.
        $exe = if ($IsWindows) { '.exe' } else { '' }
        $lefthook = Join-Path $root "bin/tools/lefthook$exe"
        Invoke-Native 'go' @('build', $toolsMod, '-o', $lefthook, 'github.com/evilmartians/lefthook/v2')
        Invoke-Native $lefthook @('install', '--force')
        # The pre-commit hook enforces this too; report it now, not at the first commit.
        Invoke-Native 'pwsh' @('-NoProfile', '-File', (Join-Path $PSScriptRoot 'check-signing.ps1'))
    }
    'verify' {
        Test-Tidy
        Invoke-Tool 'golangci-lint' @('fmt', '--diff')
        Invoke-Tool 'golangci-lint' @('run')
        Invoke-Test
        Invoke-Tool 'govulncheck' @('./...')
        Write-Host 'verify: all checks passed' -ForegroundColor Green
    }
}
