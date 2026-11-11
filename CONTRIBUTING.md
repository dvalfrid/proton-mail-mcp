# Contributing

This is a small personal project, but it is run like a real one: issues, branches, pull requests, CI and releases. This guide is the short version; the rules are in [STANDARDS.md](STANDARDS.md).

## Prerequisites

- **Go** — the version in [`go.mod`](go.mod) or newer (`winget install GoLang.Go`)
- **PowerShell 7** (`pwsh`) — the task runner; built into the GitHub runners
- **Proton Mail Bridge** — only for trying the server against a real mailbox. Tests don't need it.

## Getting started

```powershell
git clone https://github.com/dvalfrid/proton-mail-mcp.git
cd proton-mail-mcp
./tools/task.ps1 setup     # git hooks
./tools/task.ps1 verify    # everything should pass
```

## Workflow

1. **Open an issue** describing the bug or the feature (small fixes can skip this).
2. **Branch from `main`:** `git switch -c <type>/<short-name>`, e.g. `feat/list-messages`.
3. **Implement**, with tests (see [docs/testing.md](docs/testing.md)).
4. **Run `./tools/task.ps1 verify`.** Zero lint issues is the bar.
5. **Commit** with a Conventional Commit message that references the issue (`Closes #N`).
6. **Open a pull request.** The **Verify** check must be green.

Never commit to `main` directly. PRs are squash-merged; don't stack a branch on another unmerged one.

## Commit convention

Commit subjects **must** follow [Conventional Commits](https://www.conventionalcommits.org/). [Release Please](https://github.com/googleapis/release-please) reads them to write `CHANGELOG.md` and pick the next version; a commit that doesn't follow the format is silently left out of the changelog.

```
<type>(<scope>): <subject>

<optional body>

Closes #N
```

- **type** — `feat`, `fix`, `perf`, `docs`, `refactor`, `test`, `build`, `ci`, `chore`, `style`. Only `feat`, `fix` and `perf` appear in the changelog.
- **scope** — the area, lower-case: `imap`, `smtp`, `tools`, `config`, `guard`, `ci`, `readme`.
- **subject** — imperative, lower-case start, no trailing period.
- **breaking change** — `feat!:` or a `BREAKING CHANGE:` footer.

## Versions and releases

- [Semantic Versioning](https://semver.org/). Before 1.0, a breaking change bumps the minor version (0.1 → 0.2) and a feature bumps the minor too.
- Release Please keeps a release PR open. Merging it tags `vX.Y.Z`, creates the GitHub release and builds binaries for Windows, macOS and Linux with `SHA256SUMS.txt`.
- The version is embedded in the binary from the tag (`-X main.version`).

## Continuous integration

| Workflow | When | What |
|---|---|---|
| `verify.yml` | Every push to `main` and every PR | Full `verify` on Linux with `-race` (**required**), tests and build on Windows |
| `codeql.yml` | Push, PR, weekly | CodeQL for Go and Actions (when the repo is public) |
| `audit.yml` | Weekly | `govulncheck` against the current `go.sum` |
| `release-please.yml` | Push to `main` | Release PR, tag, then dispatches `release.yml` |
| `release.yml` | Dispatched per tag | Cross-platform build, checksums, upload to the release |
| Dependabot | Weekly | Grouped updates for actions, Go modules and pinned tools; no majors |

All actions are pinned to a commit SHA, with the version in a comment.

## AI tools

AI tools are welcome, and this project is largely written with Claude. If one wrote a substantial part of a change, say so with a `Co-Authored-By:` line. `CLAUDE.md` is configuration for AI coding tools: a compact map of the code and its rules. AI-written code meets the same standards and review as any other.
