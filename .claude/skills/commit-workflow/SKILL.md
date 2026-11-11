---
name: commit-workflow
description: Full sequence for shipping a bug fix or feature in proton-mail-mcp — GitHub issue, branch, implement with tests, verify, Conventional Commit, pull request. Run when starting work on a feature/bug or when ready to commit.
---

This is the sequence for every bug fix and feature. Do not skip steps or reorder them.

## 1. Open a GitHub issue before starting work

```powershell
gh issue create --title "..." --body "..." --label bug   # or --label enhancement
```

- **Bug:** incorrect behaviour, steps to reproduce, expected behaviour. Never email content or addresses.
- **Feature:** the user-visible change, why, and its permission level (read / organize / draft / send).

## 2. Branch from `main`

```powershell
git switch main; git pull; git switch -c <type>/<short-name>   # e.g. feat/list-messages, fix/imap-timeout
```

Never commit to `main` directly. Don't branch off another unmerged branch — PRs are squash-merged.

## 3. Implement with tests

- Tests per [docs/testing.md](../../../docs/testing.md): in-memory IMAP/SMTP, end to end through MCP.
- A new or changed tool: update the snapshot (`go test ./internal/server -update`) and check the diff.
- A bug fix: a test that fails without the fix.
- Touching a guardrail: stop and ask the owner first.

## 4. Verify

```powershell
./tools/task.ps1 fmt
./tools/task.ps1 verify     # zero lint issues, all tests green
```

Report the result honestly: **verifierat** (what was observed) or **hypotes** (what would confirm it). If a feature was not tried against a real Bridge, say so.

## 5. Commit

```
<type>(<scope>): <subject>

<optional body>

Closes #N
```

- **type:** `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `style`
- **scope:** `imap`, `smtp`, `tools`, `config`, `guard`, `ci`, `readme`, …
- **subject:** imperative, lower-case start, no trailing period
- Breaking change: `feat!:` or `BREAKING CHANGE:` footer

The `commit-msg` hook rejects a subject that doesn't match.

## 6. Push and open a pull request

```powershell
git push -u origin HEAD
gh pr create --fill          # title = commit subject; body from the PR template, ends with "Closes #N"
gh pr checks --watch
```

The PR is ready when **Verify** is green. The owner merges (squash). Tell them it's ready.

## 7. Docs

| What changed | Where to update |
| --- | --- |
| New tool or behaviour a user sees | `README.md` |
| Milestone item done or scope change | `ROADMAP.md` |
| A rule Claude must follow in every session | `CLAUDE.md` (only then — it is loaded every time) |
| New package or architectural decision | `CLAUDE.md` architecture table |
