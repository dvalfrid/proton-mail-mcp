#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Project task runner: the same commands locally and in CI.

.EXAMPLE
    ./tools/task.ps1 verify
    ./tools/task.ps1 test -Race
    ./tools/task.ps1 build -Version 0.2.0
    ./tools/task.ps1 installer -Version 0.2.0 -GOARCH arm64
#>
[CmdletBinding()]
param(
    [Parameter(Position = 0)]
    [ValidateSet('fmt', 'fmt-check', 'lint', 'test', 'vuln', 'tidy-check', 'verify', 'build', 'installer', 'setup')]
    [string]$Task = 'verify',

    # Run tests with the race detector (needs cgo and a C compiler; CI on Linux has both).
    [switch]$Race,

    # Version embedded in the binary by `build` (main.version).
    [string]$Version = 'dev',

    # Target for `build`, e.g. -GOOS linux -GOARCH arm64. Defaults to the host.
    [string]$GOOS = '',
    [string]$GOARCH = '',

    # Output path for `build` and `installer`.
    [string]$Out = '',

    # For `installer`: a folder that already holds the Windows exes (signed
    # in a release), LICENSE and README.md. Empty: build them first.
    [string]$Src = ''
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
    $syso = @()
    if ($exe) {
        # Windows resources: the project icon (assets/icon.ico), version
        # details and a manifest, as .syso files that go build links in.
        # Made before GOOS/GOARCH are set: go-winres runs on this machine.
        $arch = if ($GOARCH) { $GOARCH } else { (& go env GOARCH).Trim() }
        $fileVersion = if ($Version -match '^(\d+)\.(\d+)\.(\d+)') { "$($Matches[1]).$($Matches[2]).$($Matches[3]).0" } else { '0.0.0.0' }
        $copyright = (Get-Content LICENSE | Where-Object { $_ -like 'Copyright*' } | Select-Object -First 1)
        foreach ($r in @(
                @{ Dir = 'cmd/proton-mail-mcp'; Manifest = 'cli'; Name = 'proton-mail-mcp.exe'; Description = 'Proton Mail MCP server' },
                @{ Dir = 'cmd/proton-mail-mcp-setup'; Manifest = 'gui'; Name = 'proton-mail-mcp-setup.exe'; Description = 'Proton Mail MCP Setup' })) {
            Invoke-Tool 'go-winres' @('simply', '--arch', $arch, '--out', "$($r.Dir)/rsrc", '--manifest', $r.Manifest,
                '--icon', 'assets/icon.ico', '--product-name', 'Proton Mail MCP', '--file-description', $r.Description,
                '--product-version', $Version, '--file-version', $fileVersion, '--copyright', $copyright, '--original-filename', $r.Name)
            $syso += "$($r.Dir)/rsrc_windows_$arch.syso"
        }
    }
    if ($GOOS) { $env:GOOS = $GOOS }
    if ($GOARCH) { $env:GOARCH = $GOARCH }
    try {
        Invoke-Native 'go' @('build', '-trimpath', "-ldflags=-s -w -X main.version=$Version", '-o', $output, './cmd/proton-mail-mcp')
        Write-Host "Built $output ($Version)"
        if ($exe) {
            # The setup wizard, next to the server, as a windowed program
            # (no console window). Windows only for now (issue #91).
            $wizard = Join-Path (Split-Path -Parent $output) 'proton-mail-mcp-setup.exe'
            Invoke-Native 'go' @('build', '-trimpath', "-ldflags=-s -w -H=windowsgui -X main.version=$Version", '-o', $wizard, './cmd/proton-mail-mcp-setup')
            Write-Host "Built $wizard ($Version)"
        }
    } finally {
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        # Made per build: left behind, they would go into later builds.
        $syso | Remove-Item -ErrorAction SilentlyContinue
    }
}

# Builds the Windows installer (installer/proton-mail-mcp.nsi) with NSIS 3:
# makensis from MAKENSIS, PATH, or its default place on Windows.
function Invoke-Installer {
    $makensis = if ($env:MAKENSIS) { $env:MAKENSIS }
    elseif (Get-Command makensis -ErrorAction SilentlyContinue) { 'makensis' }
    else { Join-Path ${env:ProgramFiles(x86)} 'NSIS/makensis.exe' }
    if (-not (Get-Command $makensis -ErrorAction SilentlyContinue)) { throw 'makensis (NSIS 3) not found: install NSIS, or set MAKENSIS' }
    $arch = if ($GOARCH) { $GOARCH } else { 'amd64' }
    $nsisArch = @{ amd64 = 'x64'; arm64 = 'arm64' }[$arch]
    if (-not $nsisArch) { throw "no Windows installer for $arch" }

    $src = $Src
    if (-not $src) {
        $src = Join-Path $root "bin/installer-$arch"
        New-Item -ItemType Directory -Force $src | Out-Null
        $installerOut = $Out
        $script:GOOS = 'windows'; $script:GOARCH = $arch; $script:Out = Join-Path $src 'proton-mail-mcp.exe'
        try { Invoke-Build } finally { $script:Out = $installerOut }
        Copy-Item LICENSE, README.md $src
    }
    foreach ($f in 'proton-mail-mcp.exe', 'proton-mail-mcp-setup.exe', 'LICENSE', 'README.md') {
        if (-not (Test-Path (Join-Path $src $f))) { throw "$f is missing from $src" }
    }
    $output = if ($Out) { $Out } else { "bin/proton-mail-mcp_${Version}_windows_${arch}_setup.exe" }
    $output = [IO.Path]::GetFullPath((Join-Path $root $output))
    $fileVersion = if ($Version -match '^(\d+)\.(\d+)\.(\d+)') { "$($Matches[1]).$($Matches[2]).$($Matches[3]).0" } else { '0.0.0.0' }
    Invoke-Native $makensis @('-V2', "-DVERSION=$Version", "-DFILEVERSION=$fileVersion", "-DARCH=$nsisArch",
        "-DSRCDIR=$((Resolve-Path $src).Path)", "-DOUTFILE=$output", 'installer/proton-mail-mcp.nsi')
    Write-Host "Built $output ($Version, $nsisArch)"
}

switch ($Task) {
    'fmt'        { Invoke-Tool 'golangci-lint' @('fmt') }
    'fmt-check'  { Invoke-Tool 'golangci-lint' @('fmt', '--diff') }
    'lint'       { Invoke-Tool 'golangci-lint' @('run') }
    'test'       { Invoke-Test }
    'vuln'       { Invoke-Tool 'govulncheck' @('./...') }
    'tidy-check' { Test-Tidy }
    'build'      { Invoke-Build }
    'installer'  { Invoke-Installer }
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
