# Contributing

This is a small personal project, but it is run like a real one: issues, branches, pull requests, CI and releases. This guide is the short version; the rules are in [STANDARDS.md](STANDARDS.md).

## Setting up a development machine

Do this once per machine — also when you move between your own computers. Git carries the code, not the machine setup.

### 1. Install the tools

| Tool | Why | Windows |
|---|---|---|
| **Git** | | `winget install Git.Git` |
| **Go** — the `toolchain` version in [`go.mod`](go.mod) | Build and test. Go downloads that exact toolchain itself if yours is older | `winget install GoLang.Go` |
| **PowerShell 7** (`pwsh`) | The task runner `tools/task.ps1` | `winget install Microsoft.PowerShell` |
| **GitHub CLI** (`gh`) | Issues and pull requests from the terminal | `winget install GitHub.cli`, then `gh auth login` |

Nothing else: `golangci-lint`, `govulncheck` and `lefthook` are pinned in [`tools/go.mod`](tools/go.mod) and run through `go tool`, so every machine and CI use the same versions.

macOS and Linux work too (Go, Git, `pwsh` and `gh` from your package manager); the commands below are the same.

### 2. Clone, install the git hooks, verify

```powershell
git clone git@github.com:dvalfrid/proton-mail-mcp.git   # contributors: clone your fork
cd proton-mail-mcp
./tools/task.ps1 setup     # git hooks: signing, fmt + lint before commit, commit-message check
./tools/task.ps1 verify    # everything should pass
```

The git hooks live in `.git/hooks`, which is not part of the repository: run `setup` on every new clone.

### 3. Sign your commits

Commits on `main` must be signed, and the ruleset checks every commit in a pull request: one unsigned commit in your branch blocks the merge, even though pull requests are squash-merged. The pre-commit hook and `setup` refuse until signing is configured, so set it up before your first commit on a new machine, with a separate SSH key per machine:

```powershell
ssh-keygen -t ed25519 -C "git signing" -f "$HOME\.ssh\id_ed25519_signing"
gh auth refresh -h github.com -s admin:ssh_signing_key
gh ssh-key add "$HOME\.ssh\id_ed25519_signing.pub" --type signing --title "Git signing (<machine name>)"
git config --global gpg.format ssh
git config --global user.signingkey "$HOME/.ssh/id_ed25519_signing.pub"
git config --global commit.gpgsign true
git config --global tag.gpgsign true
```

If the key has a passphrase, the SSH agent must hold it, or commits (also those an AI assistant makes for you) fail. On Windows, in an elevated PowerShell: `Set-Service ssh-agent -StartupType Automatic; Start-Service ssh-agent`; then `ssh-add "$HOME\.ssh\id_ed25519_signing"` and `git config --global gpg.ssh.program "C:/Windows/System32/OpenSSH/ssh-keygen.exe"`.

Check with `git log --show-signature -1` after your next commit. Recommended: GitHub → Settings → SSH and GPG keys → **Flag unsigned commits as unverified** (vigilant mode).

### 4. Optional: a real mailbox

Tests never need Proton or Bridge. To try the server against your own mailbox, install [Proton Mail Bridge](https://proton.me/mail/bridge) on that machine and follow [README — Setup](README.md#setup): a `.env` with the Bridge username and password, and the certificate exported from Bridge. Both stay on the machine — `.env` and certificates are git-ignored and must never be committed (the repository is public, see [STANDARDS.md](STANDARDS.md#public-repository)).

### Moving between machines

Before you leave one machine, make sure nothing is only there:

```powershell
git status          # nothing uncommitted
git stash list      # nothing stashed
git status -sb      # first line shows no [ahead N]: everything is pushed
```

On the other machine:

```powershell
git fetch --prune
git switch main; git pull                   # if your pull request has been merged meanwhile
git switch <your-branch>; git pull          # to continue on a branch that is still open
```

Merged branches are deleted on GitHub automatically; delete your local copy with `git branch -d <branch>`.

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
