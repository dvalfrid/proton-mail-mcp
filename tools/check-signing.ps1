#!/usr/bin/env pwsh
# pre-commit hook (and `task.ps1 setup`): commits must be signed, because the
# main ruleset rejects a pull request with any unsigned commit in it. Failing
# here is cheaper than finding out at merge time and re-signing the branch.

$problems = @()

if ((git config --get commit.gpgsign) -ne 'true') {
    $problems += 'commit.gpgsign is not true'
}

$key = git config --get user.signingkey
if (-not $key) {
    $problems += 'user.signingkey is not set'
} elseif ((git config --get gpg.format) -eq 'ssh' -and -not $key.StartsWith('key::')) {
    # An SSH signing key is a path to a key file unless given inline (key::...).
    $path = $key -replace '^~', $HOME
    if (-not (Test-Path -LiteralPath $path)) {
        $problems += "user.signingkey points to a missing file: $key"
    }
}

if ($problems.Count -eq 0) { exit 0 }

Write-Host 'Commit signing is not set up on this machine:' -ForegroundColor Red
$problems | ForEach-Object { Write-Host "  - $_" }
Write-Host ''
Write-Host 'Commits on main must be signed; an unsigned commit blocks the pull request.'
Write-Host 'Set it up once per machine: CONTRIBUTING.md, "3. Sign your commits".'
exit 1
