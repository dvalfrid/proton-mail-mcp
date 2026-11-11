# Code Standards

This document defines the coding, security and testing standards for this project. All contributors and AI assistants must follow these rules when writing, modifying, or reviewing code.

proton-mail-mcp is a small Go program with one job: give an AI assistant controlled access to a Proton Mail inbox through Proton Mail Bridge. It handles the owner's email and the Bridge password, so the [security guardrails](#security-guardrails) outrank every other rule here.

## Contents

- [Tools and commands](#tools-and-commands)
- [Go](#go)
- [Security guardrails](#security-guardrails)
- [Testing](#testing)
- [Dependencies](#dependencies)
- [AI-generated code](#ai-generated-code)

---

## Tools and commands

All tasks go through one script, so local runs and CI do the same thing. It needs PowerShell 7 (`pwsh`), which runs on Windows, Linux and macOS.

| Purpose | Command |
|---|---|
| Format (modifies files) | `./tools/task.ps1 fmt` |
| Check formatting (CI) | `./tools/task.ps1 fmt-check` |
| Lint (`golangci-lint`, includes `go vet`) | `./tools/task.ps1 lint` |
| Run all tests | `./tools/task.ps1 test` (add `-Race` where cgo is available, as in CI) |
| Known vulnerabilities (`govulncheck`) | `./tools/task.ps1 vuln` |
| Full verification (tidy, fmt-check, lint, test, vuln) | `./tools/task.ps1 verify` |
| Build `bin/proton-mail-mcp.exe` | `./tools/task.ps1 build` |
| First-time setup (git hooks) | `./tools/task.ps1 setup` |

Tool versions (`golangci-lint`, `govulncheck`, `lefthook`) are pinned in [`tools/go.mod`](tools/go.mod) and run with `go tool -modfile=tools/go.mod …`. Nothing needs to be installed beyond Go, and everyone, including CI, runs the same versions. Dependabot keeps them current.

Run `./tools/task.ps1 verify` before every commit. The `lefthook` pre-commit hook runs the fast part of it (fmt-check and lint).

---

## Go

### Formatting

- `gofmt` and `goimports`, run through `golangci-lint fmt`. Never format by hand.
- Imports in three groups: standard library, third party, this module (`github.com/dvalfrid/proton-mail-mcp/...`). `goimports` does it.

### Naming

Follow [Effective Go](https://go.dev/doc/effective_go#names) and the [Go code review comments](https://go.dev/wiki/CodeReviewComments) without exception:

| Kind | Convention | Example |
|---|---|---|
| Exported | `PascalCase` | `ListMessages`, `Config` |
| Unexported | `camelCase` | `fetchEnvelope`, `bridgeAddr` |
| Initialisms | all caps | `UID`, `IMAP`, `SMTP`, `TLS`, `MCP`, `ID`, `URL` (`messageUID`, not `messageUid`) |
| Packages | short, lower case, no underscores | `config`, `mail`, `tools` |
| Errors | `ErrX` for sentinels, `XError` for types | `ErrSendDisabled` |

No stutter: `config.Config` is fine, `config.ConfigLoader` is not.

### Documentation comments

- Every package has a package comment (`// Package mail …`) that describes its responsibility and notable design decisions.
- Every exported identifier has a doc comment that starts with its name.
- Unexported helpers need no comment if the name says it.
- Explain *why*, not *what*. Never restate the signature in prose.

### Error handling

- Fallible functions return `error` as the last value. No `panic` outside `main` and programmer-error checks at start-up.
- Wrap with context: `fmt.Errorf("select folder %q: %w", name, err)`. Compare with `errors.Is` / `errors.As`, never by string.
- An error returned to Claude (a tool result) says what failed in plain words and never contains the Bridge password, a full message body, or raw protocol dumps.
- Don't ignore errors. If one really doesn't matter, assign it to `_` with a comment saying why.

### Context and timeouts

- Every function that does I/O takes a `context.Context` as its first parameter and respects cancellation.
- Every network operation has a timeout. Bridge on localhost answers in milliseconds; a hang means something is wrong, and Claude must get an error, not wait forever.

### Logging

- `log/slog`, to **stderr only**. stdout carries the MCP protocol: one stray `fmt.Println` breaks the connection. `golangci-lint` (`forbidigo`) rejects `fmt.Print*` and `print`/`println`.
- Log metadata (folder, UID, counts, durations), never content: no message bodies, no subjects, no passwords. See [Security guardrails](#security-guardrails).

### Concurrency and state

- No package-level mutable state. State lives in a struct created in `main` and passed down.
- An IMAP connection is not safe for concurrent use: one owner, or behind a `sync.Mutex`.
- The test suite runs with `-race` in CI.

### Package layout

- `cmd/proton-mail-mcp/` holds `main` only: config, wiring, start-up.
- Everything else is under `internal/`, so no other module can import it. No `pkg/`.
- One responsibility per package. Mail protocol code doesn't know about MCP; MCP tool code doesn't speak IMAP.

### Linter suppressions

Every `//nolint` names the linter and says why: `//nolint:gosec // G402: Bridge's self-signed cert, localhost only`. `nolintlint` enforces both. Don't silence a lint without explaining why it doesn't apply.

### Go version

The `go` line in `go.mod` is the minimum supported version. Raise it deliberately, not to make one call compile.

The `toolchain` line (in `go.mod` and `tools/go.mod`) is the Go release that builds, tests and ships the binary, and CI installs exactly that. Keep it on the newest patch release: standard-library security fixes arrive as patch releases, and `govulncheck` fails the build when the toolchain has a known vulnerability the code reaches.

---

## Security guardrails

These are hard rules. A change that breaks one is not merged, whatever it adds. Each has a test that fails if it's broken (see [Testing](#testing)).

### What Claude may do

| Level | Tools | Status |
|---|---|---|
| Read | list folders, list/search messages, read a message | Allowed |
| Organize | move, mark read/unread, flag, label | Allowed (undoable) |
| Draft | create, update draft in `Drafts` | Allowed |
| Send | `send_draft`: send an *existing* draft | Gated: see below |
| Delete permanently | — | **Never** |

### Sending

1. The **only** way to send is `send_draft`, which sends a draft that already exists in `Drafts`. No tool ever takes recipients and a body and sends them in one step: the owner must be able to read what goes out.
2. `send_draft` is refused unless `PROTON_ALLOW_SEND=1`. Refused means the tool returns an error, not that it's left out of the tool list. Claude should see that sending exists but is switched off.
3. The MCP client is configured to **always ask** before `send_draft` runs (see README). That approval comes from the client, outside Claude's control, which is why the rule is in the client and not only here.
4. SMTP code lives in one package. `forbidigo` rejects SMTP send calls anywhere else.

### Deletion

- No IMAP `EXPUNGE` / `UID EXPUNGE`, ever. `forbidigo` rejects calls to `Expunge`/`UIDExpunge`.
- "Delete" means move to `Trash`. Proton empties Trash on its own schedule; the owner can still restore.

### Email content is untrusted

Anyone can send the owner an email, so mail content can contain instructions aimed at the AI (prompt injection).

- Tool output marks message content as untrusted: headers and body are returned inside a clearly delimited block labelled as data from the sender, not as instructions.
- HTML is converted to text; scripts, styles and remote resources are dropped, never fetched.
- Attachments are listed (name, type, size), not opened or executed.
- Output size is capped per message and per call.
- The send guard above is the hard stop: even a fooled assistant can only create a draft.

### Secrets

- The Bridge password lives only in `.env` (git-ignored) or the environment. Never in code, tests, logs, error messages or tool output.
- Claude Code in this repo is denied reading `.env` (`.claude/settings.json`).
- Tests use generated credentials against in-memory servers.

### Public repository

The repository is public, so everything committed is published: code, tests, test data, docs, commit messages, issues and pull requests.

- No secrets of any kind (passwords, tokens, keys, certificates, `.env` contents). GitHub secret scanning with push protection is on as a last line of defence, not as the first.
- No real email: test messages, addresses and names are invented (use the reserved `example.com` / `example.org` / `example.net` domains) or fully anonymised before commit.
- No personal details: real addresses, local paths with user names, machine names, unreviewed logs from a real run.
- Bug reports and issues describe a message instead of quoting it (the issue template says so).

### Transport

- Connect only to Bridge on `127.0.0.1` / `localhost`. A non-local host is refused at start-up.
- STARTTLS is required, and the server is always verified against the certificate exported from Bridge (`PROTON_BRIDGE_CERT`, required). Verification is **never** skipped, not even on localhost: when Bridge isn't running, any local program can listen on its port, and without verification it would receive the Bridge password at login. No `InsecureSkipVerify` in production code.

---

## Testing

Full strategy: [docs/testing.md](docs/testing.md). The rules:

- Tests sit next to the code (`foo_test.go`), table-driven where it fits.
- No test needs Proton or Bridge. IMAP and SMTP are tested against in-memory servers (`go-imap/v2`'s `imapmemserver`, `go-smtp`'s server).
- Tools are tested end to end through MCP: an in-memory client calls the tool on an in-memory server.
- Every guardrail has a test that fails if the guardrail breaks.
- Parsing of mail content (untrusted input) has fuzz tests.
- Tests against a real Bridge are behind the `live` build tag and run by hand only, never in CI.
- A bug fix comes with a test that fails without the fix.

---

## Dependencies

Standard library first. Approved:

| Module | For |
|---|---|
| `github.com/modelcontextprotocol/go-sdk` | MCP server (official SDK) |
| `github.com/emersion/go-imap/v2` | IMAP client, in-memory IMAP server for tests. Still tagged beta upstream, but it is the maintained line (v1 is not); pinned in `go.mod`, upgrades go through the full test suite |
| `github.com/emersion/go-message` | MIME parsing |
| `github.com/emersion/go-smtp`, `github.com/emersion/go-sasl` | SMTP client, SMTP server for tests |

Anything else needs explicit approval from the owner, with a reason in the PR. `go mod tidy` must leave `go.mod` and `go.sum` unchanged (checked in CI).

---

## AI-generated code

AI assistants such as Claude may be used during development, but all generated code must follow the same standards as human-written code:

- Keep solutions simple and concrete; avoid unnecessary interfaces, generics, or layers. An interface earns its place when there are two implementations (for example a real and a test one).
- Generated code must pass `./tools/task.ps1 verify` with zero warnings.
- Follow all naming, formatting, package-layout and security rules in this document.
- Do not add dependencies without explicit approval.
- Do not restructure packages, rename files, or change architecture unless explicitly instructed.
- Never weaken a guardrail or its test to make something pass. If a guardrail is in the way, stop and ask.
- All AI-generated code is reviewed with the same scrutiny as human-written code.
