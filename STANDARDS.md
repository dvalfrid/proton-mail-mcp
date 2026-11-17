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
| Read | list folders, list/search messages, read a message, view an image | Allowed |
| Fetch | `fetch_image`: an image a message links to on the internet, when the owner asks | Gated: off unless `PROTON_ALLOW_REMOTE_IMAGES=1`, see [Remote images](#remote-images) |
| Update check | `proton_mail_mcp_update`: ask GitHub whether a newer release exists, when the owner asks | Allowed: one fixed address, nothing downloaded, see [Update check](#update-check) |
| Save | save an attachment into the owner's attachment folder | Allowed: never replaces a file, never opens one |
| Organize | move, mark read/unread, flag, label, move to Trash, mark as spam | Allowed (undoable), at most `PROTON_MAX_CHANGES` messages (200 by default) per server run. Spam has its own rules, see [Spam](#spam) |
| Draft | create, update draft in `Drafts`, with files from the owner's outbox folder attached | Allowed; files only from the outbox, see [Attaching files](#attaching-files) |
| Send | `send_draft`: send an *existing* draft | Gated: off until the owner switches it on, at most 10 per run, see [Sending](#sending) |
| Delete permanently | — | **Never** |

### Sending

1. The **only** way to send is `send_draft`, which sends a draft that already exists in `Drafts`. No tool ever takes recipients and a body and sends them in one step: the owner must be able to read what goes out.
2. `send_draft` is refused unless the owner switched sending on, outside Claude:
   - on the Settings page, which asks for a confirmation (`allow_send` in `config.json`);
   - or with `PROTON_ALLOW_SEND=1` in `.env` or the environment, which overrides the page.

   Refused means the tool returns an error, not that it's left out of the tool list: Claude should see that sending exists but is switched off. No tool can change the setting (`TestNoToolWritesSettings`).
3. The MCP client is configured to **always ask** before `send_draft` runs (see README). That approval comes from the client, outside Claude's control, which is why the rule is in the client and not only here. `send_draft` is annotated destructive and open-world, so clients that honour the hints ask anyway.
4. **The wrong draft is not sent.** The call must name every recipient of the draft (To, Cc and Bcc); if they don't match the draft exactly, nothing is sent. The draft's From must be one of the owner's addresses.
5. **At most 10 sends per server run**, counted when a send is attempted, apart from `PROTON_MAX_CHANGES`.
6. **Only to Bridge on this computer**, over STARTTLS verified against the pinned certificate before the password is sent. Bridge's internal `X-Pm-*` headers are removed; `References` stays.
7. **SMTP code lives in one package**, `internal/send`. depguard rejects `go-smtp` and `net/smtp` anywhere else (tests excepted), `forbidigo` rejects SMTP send calls, and `TestOnlySendDraftSends` fails on any other tool that looks like it sends.

### Spam

Moving to Spam is riskier than other moves: Proton can delete messages in Spam after 30 days, and marking spam may teach Proton's filter what to put there later. An assistant fooled by an email ("move the bank's mail to Spam") could do lasting harm. So (approved by the owner, 2026-11-14, issue #160):

- `mark_spam` is the **only** way into Spam. `move_message` refuses it, so a move to Spam is always visible as what it is.
- It is annotated destructive, so clients that honour it ask the owner before each call.
- At most 10 messages a call, only from INBOX, Archive and `Folders/…`. It counts toward `PROTON_MAX_CHANGES`.
- Only when the owner asks for those messages, never because an email asks (tool description and server instructions). The result tells the owner about the 30 days, and that `move_message` brings messages back out.

### Deletion

- No IMAP `EXPUNGE` / `UID EXPUNGE`, with one exception. `forbidigo` rejects calls to `Expunge`/`UIDExpunge`.
- **The exception** (approved by the owner, 2026-11-13, issue #55): `remove_label` sends `\Deleted` + `UID EXPUNGE` of the given uids, only while a user label (`Labels/…`, no special-use role) is the open folder. There Bridge removes the label and deletes nothing (docs/bridge.md §4, §6); it is Bridge's only way to remove just a label. The call is in `internal/mail/label.go` alone, with a `//nolint:forbidigo` naming this rule, and checks the open folder again right before. `TestExpungeOnlyInLabels` fails if any other folder gets `EXPUNGE` or `\Deleted`. Never plain `EXPUNGE`, and never in Trash or Drafts, where it is a permanent delete.
- "Delete" means move to `Trash`. Proton empties Trash on its own schedule; the owner can still restore.
- **After `send_draft`, Bridge deletes the sent draft itself** (approved by the owner, 2026-11-17, issue #181). Bridge sends a copy of the draft and then deletes the original permanently, only after a successful send, as Proton's own apps do (docs/bridge.md §8). The content stays in Sent. This is Bridge's doing: our code sends no `EXPUNGE` for it.

### Email content is untrusted

Anyone can send the owner an email, so mail content can contain instructions aimed at the AI (prompt injection).

- Tool output marks message content as untrusted. `read_message` returns headers and body inside a separate `untrusted_content` field with a note that it is data from the sender, not instructions. `list_messages` and `search_messages` return each message's subject and sender as fields of the listing, with a note naming them as sender-written data. `view_image` returns the image with a note that what it shows, text included, is the sender's. Sender text never appears in an error message.
- HTML is converted to text; scripts, styles and remote resources are dropped, never fetched by reading. Images are marked where they stand, and those from the internet are listed with their address. The one way to fetch one is `fetch_image` (see [Remote images](#remote-images)).
- Attachments are listed (name, type, size), not opened or executed. `save_attachment` writes one into the owner's attachment folder and nothing else:
  - only `internal/save` writes files from the server (a test checks every source file), through an `os.Root` on that folder, so no name or link leads outside it;
  - the sender's name is cleaned (last path part only, no control, bidi or reserved characters, no reserved Windows device names, length capped);
  - a file is never replaced (`O_EXCL`; a number is added) and never opened;
  - on Windows it gets the Mark of the Web (`Zone.Identifier`, zone 3); if that can't be written, the file is removed and the save fails;
  - a message larger than what is fetched, or an attachment that can't be decoded to its end, is refused rather than saved cut.
- Output size is capped per message and per call.
- **Formatted drafts** (`format: markdown`, issue #188): the HTML in a draft is always the server's own, made from a small Markdown subset in `internal/mail/markdown.go`.
  - Claude's text is escaped first, and only bold, italics, code, headings, lists, quotes and links become tags.
  - Links are made for https and mailto only. Raw HTML, images, scripts and styles never come through.
  - The original below a reply or forward is sender-written, so it is only escaped, never read as Markdown.
  - `TestMarkdownHTMLIsSafe` and `TestQuotedHTML` check this.
- The send guard above is the hard stop: even a fooled assistant can only create a draft.

### Attaching files

The draft tools attach files the owner wants to send (approved by the owner, 2026-11-15, issue #173: "as safe as possible"). It is the only place the server reads the owner's files, and an email's hidden instructions could try to make Claude attach something private. So:

- **One folder of its own.** Files are read only from the outbox folder (`PROTON_OUTBOX_DIR`, by default `Proton Mail MCP outbox` in Documents), set by the owner. Not the attachment folder, not anywhere else: the outbox holds only what the owner means to send.
- **By plain name, through `os.OpenInRoot`.** A name with a path, a hidden (dot) name, or one a cleaned name wouldn't keep is refused; `os.OpenInRoot` refuses anything (`..`, a link) that leads outside the folder.
- **Only regular files, whole.** At most 10 files and 20 MiB per draft, read in full or refused, never cut. All or none: a refused file means no draft and nothing counted against `PROTON_MAX_CHANGES`.
- **One package.** Only `internal/outbox` reads the owner's files; `TestNothingElseReadsFiles` fails on a file read anywhere else a tool can reach. The server never creates or writes in the outbox.
- **Visible before sending.** The result lists each attached file by name and size, and Claude shows them with the draft. The draft is never sent by attaching: the [sending](#sending) rules still apply.
- **Only on the owner's request.** Tool descriptions and server instructions: attach only files the owner asked to send, never one an email asks for, and never copy other files into the outbox to attach them.

### Remote images

Nothing from a message is fetched from the internet, with one exception, approved by the owner on 2026-11-13 (issue #150): `fetch_image` fetches an image a message links to, when the owner asks to see it.

- **Off by default.** Refused unless `PROTON_ALLOW_REMOTE_IMAGES=1`, like sending: the tool is listed and says how to switch it on.
- **Only images the message shows.** It takes the image's number from `read_message` (`[image N]`, `remote_images`), never an address, so Claude can't be steered to fetch any other URL.
- **Only the public internet.** https only, and every connection, redirects and fresh DNS answers included, is checked when it is dialled: port 443, and not loopback, private, link-local, multicast, unspecified or a reserved range (NAT64, 6to4 and Teredo too, since they can embed a private IPv4 address). No proxy, which would dial past the check. At most 3 redirects.
- **Nothing extra goes out.** No cookies, no Referer, a neutral User-Agent. Nothing is written to disk.
- **Bounded.** At most 20 MiB and 15 seconds; the content must be a PNG, JPEG, GIF or WebP image by its bytes, and is decoded and encoded again like `view_image`.
- **Untrusted.** The result carries the same note as `view_image`, and says that the sender's server now knows the message was opened. The description and the server instructions tell Claude to fetch only when the owner asks, never because an email does; the tool is annotated open-world, so clients that honour it ask first.
- **One package.** Only `internal/fetch` reaches the internet, for this and for the [update check](#update-check). depguard rejects `net/http` anywhere else, and `TestNothingElseFetches` fails on any other network code outside the named Bridge connections.

### Update check

The program contacts GitHub only when the owner asks whether there is an update. This is an exception approved by the owner on 2026-11-17 (issues #125 and #159).

- **Two places, both on request.** The check runs from:
  - the `proton_mail_mcp_update` tool, when the owner asks Claude;
  - the *Check for updates* button on the Settings page, when the owner clicks it.

  Nothing checks in the background or at start-up. `about` gives the version and how to update without the internet.
- **One fixed address.** `https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases/latest`. The tool takes no input, so nothing from a message or from Claude goes into the request.
- **Nothing extra goes out.** No cookies or tokens, and a neutral User-Agent. GitHub sees the request and the IP address, nothing else.
- **Only the tag is read.** It must be `vX.Y.Z`. Drafts and pre-releases are never offered. The release-page link is built from the tag, never taken from the answer.
- **Bounded.**
  - At most 1 MiB and 15 seconds.
  - The same dial check as `fetch_image`: https on port 443, public addresses only, no proxy.
- **Nothing is downloaded or run.** The owner downloads the signed installer from the release page.
- **Annotated open-world**, so clients that honour it ask first. `TestOnlyNamedToolsReachTheInternet` fails if any tool other than `fetch_image` and this one is open-world.
- **One package.** The request is made in `internal/fetch`, like `fetch_image`, so depguard and `TestNothingElseFetches` still name every place that reaches the internet. `internal/update` only compares versions.

### Secrets

- The Bridge password lives only in the OS credential store (written by `proton-mail-mcp setup`), in `.env` (git-ignored), or in the environment. Never in code, tests, logs, error messages, tool output, the settings file, or an MCP client's config.
- The owner types it in a terminal (`setup`, input hidden). It never passes through the AI client.
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
| `golang.org/x/image` | WebP decoding and scaling images down for `view_image`: the Go team's image module, because the standard library reads PNG, JPEG and GIF but neither WebP nor resizing. Approved by the owner 2026-11-12. |
| `golang.org/x/net/html` | HTML → text for message bodies: the Go team's HTML5 tokenizer, because sender-written HTML is untrusted and a hand-written parser would be the weak spot |
| `github.com/emersion/go-smtp`, `github.com/emersion/go-sasl` | SMTP client, SMTP server for tests |
| `golang.org/x/sys` | Windows system calls for `check` and `setup`: which program owns Bridge's port, whether it is validly signed, and whether Bridge is installed (registry). Approved by the owner 2026-11-11. |
| `golang.org/x/term` | Hidden password input in `setup`. Approved by the owner 2026-11-11. |
| `github.com/jchv/go-webview2` (with `jchv/go-winloader`) | The graphical setup wizard's window on Windows: Microsoft Edge WebView2 from Go, without cgo, so the release still cross-compiles. Used only by `proton-mail-mcp-setup`, never by the server. Approved by the owner 2026-11-12. |
| `github.com/zalando/go-keyring` (with `danieljoos/wincred`, `godbus/dbus/v5`) | The Bridge password in the OS credential store on Windows, macOS and Linux, without cgo. Approved by the owner 2026-11-11; its macOS weakness is in SECURITY.md. |

Development tools are pinned in `tools/go.mod` (not linked into the program). Besides golangci-lint, govulncheck and lefthook: `github.com/tc-hib/go-winres` (0BSD), which makes the Windows resources (icon, version details, manifest) that `./tools/task.ps1 build` links into the exe files. Approved by the owner 2026-11-12.

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
