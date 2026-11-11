#!/usr/bin/env pwsh
# commit-msg hook: the subject must be a Conventional Commit, because
# release-please builds the changelog and the version from it.
param([Parameter(Mandatory)][string]$MessageFile)

$subject = (Get-Content -LiteralPath $MessageFile -TotalCount 1).Trim()

# Git's own merge/revert/fixup subjects are left alone.
if ($subject -match '^(Merge|Revert|fixup!|squash!|amend!) ') { exit 0 }

$pattern = '^(feat|fix|perf|docs|refactor|test|build|ci|chore|style)(\([a-z0-9][a-z0-9-]*\))?!?: [^A-Z].*[^.]$'
if ($subject -cmatch $pattern) { exit 0 }

Write-Host "Commit subject is not a Conventional Commit:" -ForegroundColor Red
Write-Host "  $subject"
Write-Host ""
Write-Host "Expected: <type>(<scope>): <subject>   e.g. feat(imap): add list_messages tool"
Write-Host "  type: feat fix perf docs refactor test build ci chore style"
Write-Host "  subject: imperative, lower-case start, no trailing period"
exit 1
