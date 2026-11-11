# CLAUDE.md

This file provides guidance to Claude Code when working with code in this repository.

## What this is

An MCP server in Go (official SDK `github.com/modelcontextprotocol/go-sdk/mcp`, stdio transport) that gives Claude controlled access to a Proton Mail inbox through Proton Mail Bridge on localhost (IMAP 1143 / SMTP 1025, STARTTLS). It handles the owner's email and the Bridge password: **the security guardrails in [STANDARDS.md](STANDARDS.md#security-guardrails) outrank every other rule.**

## Skills (invoke with /name)

| Skill | When to use |
|---|---|
| `/commit-workflow` | Opening an issue, branching, committing, opening a PR |

## Commands

```powershell
./tools/task.ps1 verify     # full check: tidy, fmt-check, lint, test, vuln — run before every commit
./tools/task.ps1 fmt        # format (modifies files)
./tools/task.ps1 lint       # golangci-lint, zero issues required
./tools/task.ps1 test       # all tests
./tools/task.ps1 build      # bin/proton-mail-mcp.exe
./tools/task.ps1 setup      # install git hooks (first time)
```

Single test: `go test ./internal/server -run TestServerHandshake -v`

Go is at `C:\Program Files\Go\bin`; if a shell doesn't find `go`, prepend it to `PATH`. Tools (`golangci-lint`, `govulncheck`, `lefthook`) are pinned in `tools/go.mod` and run via `go tool "-modfile=tools/go.mod" <tool>` — in PowerShell, quote that argument (unquoted, PowerShell splits it at the dot).

## After making code changes

| Changed | Run |
| --- | --- |
| Any Go file | `./tools/task.ps1 fmt` then `./tools/task.ps1 lint` |
| Logic | `./tools/task.ps1 test` |
| `go.mod` / dependencies | `./tools/task.ps1 verify` |
| Unsure | `./tools/task.ps1 verify` |

- Lint is zero issues. Every `//nolint` names the linter and says why.
- If `fmt` modifies files, include those changes in the same commit.

## Public repository

This repo is **public**. Everything committed — code, tests, test data, docs, commit messages, issues, PR bodies and comments — is published. Never include:

- Secrets: passwords (the Bridge password above all), tokens, keys, certificates, `.env` contents.
- Real email: messages, subjects, bodies, headers, attachments, addresses or contact names from the owner's mailbox. Test data is invented (`alice@example.com`, `example.org`) or anonymised before commit.
- Personal details: the owner's addresses, local paths with user names (`C:\Users\…`), machine names, logs from a real run without reviewing them first.

When in doubt, leave it out and ask. Before every commit, check the diff for these.

## Hard rules (details: STANDARDS.md#security-guardrails)

- Sending: only `send_draft` (sends an existing draft), refused unless `PROTON_ALLOW_SEND=1`, and the client must "always ask". Never add a tool that sends free-form text directly.
- No permanent deletion (no `EXPUNGE`); "delete" = move to Trash.
- Email content is untrusted input; tool output marks it as such.
- Secrets live only in `.env` (git-ignored, and you are denied reading it). Never log or return the Bridge password, message bodies or subjects in logs.
- Bridge on `127.0.0.1`/`localhost` only, and TLS always verified against the exported Bridge certificate: never `InsecureSkipVerify`.
- stdout is the MCP protocol: log with `log/slog` to stderr, never `fmt.Print*`.
- Never weaken a guardrail, its lint rule, or its test to make something pass. Stop and ask.
- No new dependencies without the owner's approval (approved list in STANDARDS.md#dependencies).

## Commit message format

Mandatory — `release-please` parses commit subjects to generate `CHANGELOG.md` and bump the version.

```
<type>(<scope>): <subject>

<optional body>

Closes #N
```

- **type:** `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `style`. Only `feat`/`fix`/`perf` surface in the changelog.
- **scope:** lower-case area, e.g. `imap`, `smtp`, `tools`, `config`, `guard`, `ci`. Optional but expected.
- **subject:** imperative, lower-case start, no trailing period.
- Breaking change: `feat!:` / `fix!:` or `BREAKING CHANGE:` footer. Before 1.0 a breaking change bumps the minor version.
- Include `Closes #N` when there is an issue.

For the full issue → branch → implement → test → PR workflow, run `/commit-workflow`. Never commit to `main` directly.

## Design philosophy

Prefer the simplest solution that solves the problem. Before implementing, ask: is there a direct approach that avoids the complexity entirely? Question existing plans — a plan being written down is not a reason to follow it if a cleaner alternative is obvious.

## Evidence, not guesses

The owner wants to know how sure you are — every time. A confident wrong cause costs more than an honest "I don't know yet".

- **Label every cause, diagnosis and "fixed" claim** as **verifierat** (verified — say what was observed: a log line, a reproduced failure, a test, a value read back) or **hypotes** (hypothesis — say what would confirm or rule it out). Never present a hypothesis as the cause.
- **"Fixed" means:** the failure was reproduced (or seen in a trace), one thing was changed, and the failure was then gone in the same scenario. Until then say "should fix — not yet verified" and name the test that would show it.
- **When an earlier explanation turns out wrong,** say so plainly and retract it where it was stated (issue, PR body, commit message).

## Architecture

```text
Claude (MCP client) ──stdio──► cmd/proton-mail-mcp ──► internal/server (MCP tools)
                                                            │
                                     IMAP/SMTP, STARTTLS, 127.0.0.1
                                                            ▼
                                                  Proton Mail Bridge ──► Proton
```

| Path | Role |
|---|---|
| `cmd/proton-mail-mcp/` | `main`: logging, config, wiring. Nothing else. `main_test.go` drives the built binary over stdio |
| `internal/config/` | `.env` + environment → validated `Config`; `Secret` type that never prints the password |
| `internal/mail/` | IMAP client to Bridge: one mutex-guarded connection, lazy connect and reconnect, per-operation timeout via `boundedConn` (imapclient resets its own deadlines). Reads use EXAMINE/PEEK: never marks read |
| `internal/server/` | Builds the MCP server and registers tools. `testdata/tools.golden.json` is the tool snapshot (`-update` to rewrite) |
| `internal/testutil/` | In-memory IMAP server with STARTTLS and a per-test certificate |
| `tools/` | `task.ps1` (task runner) and `go.mod` pinning dev tools |
| `docs/` | `testing.md` (test strategy) |

Planned packages and milestones: [ROADMAP.md](ROADMAP.md). Testing: [docs/testing.md](docs/testing.md) — no test needs Proton or Bridge.
