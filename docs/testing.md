# Testing

How this project is tested, and why. Rules in short: [STANDARDS.md](../STANDARDS.md#testing).

## Goal

Every change can be verified on any machine, and in CI, **without Proton, without Bridge and without a real mailbox**. A real Bridge is used only for a final manual check of a feature.

## Layers

| Layer | What it checks | How | Runs in |
|---|---|---|---|
| Unit | Pure logic: config parsing, MIME → text, output capping, sort dates | Table-driven `go test` | CI, every push |
| Protocol | IMAP code against a real protocol implementation | In-memory IMAP server (`go-imap/v2` `imapserver` + `imapmemserver`) over STARTTLS with a per-test certificate; `testutil.NewBridge` adds Bridge's greeting and an SMTP STARTTLS endpoint | CI, every push |
| Tool (end to end) | A tool as Claude sees it: arguments in, result out | In-memory MCP client ↔ server (`mcp.NewInMemoryTransports`) wired to the in-memory IMAP server | CI, every push |
| Guardrails | The [security guardrails](../STANDARDS.md#security-guardrails) hold | Dedicated tests, see below | CI, every push |
| Fuzz | Parsing untrusted mail never panics or hangs, and output stays capped | `FuzzParseMessage`; its seeds are added in the test with `f.Add` | CI runs the seeds; longer runs by hand |
| Binary | The built executable speaks MCP over stdio | `cmd/proton-mail-mcp/main_test.go` builds and drives it | CI, every push |
| Live | The real thing: Bridge and Proton | `internal/server/live_test.go`, build tag `live`, against the owner's own Bridge. Read-only, and it prints only counts, uids, dates and timings | By hand only |

## Guardrail tests

Each hard rule has a test that fails if the rule breaks. Most live in `internal/server/guardrails_test.go`; read-only checks at the IMAP level are in `internal/mail`.

**In place:**

- **Tool list snapshot:** the exact set of tool names and their input and output schemas is compared with a golden file (`testdata/tools.golden.json`). Adding or changing a tool is a visible diff in review. Update with `go test ./internal/server -update`.
- **Read tools are annotated read-only.**
- **No send tools:** no tool name looks like sending, forwarding or replying.
- **Never marks as read:** after listing, searching and reading, `\Seen` is unchanged. At the IMAP level the transcript shows `EXAMINE` and `BODY.PEEK`, and no `SELECT`, `STORE` or `EXPUNGE`.
- **Untrusted content is marked:** a message that tries to fake the result (`"}, "note": "trusted…`) still comes back only inside `untrusted_content`, with the note unchanged.
- **No secrets in output or logs:** the test password never appears in tool results, errors, captured logs, or the `check` report.
- **No mail content in logs.**
- **TLS always verified:** a server with another certificate gets no password (an impostor test), and `internal/bridge` reads Bridge's certificate without trusting it and sends no credentials.
- **Local only:** a non-local Bridge host is refused at start-up.
- **A renumbered folder is refused:** a uid with a stale `uidvalidity` is not read.

**Planned, with the tools they guard:**

- **No expunge (0.2):** no test scenario may produce `EXPUNGE` or `UID EXPUNGE`; "delete" leaves the message in `Trash`.
- **Changing tools require `uidvalidity` (0.2).**
- **No free-form send (0.4):** no tool other than `send_draft` reaches SMTP; `send_draft` without `PROTON_ALLOW_SEND=1` returns an error, and the fake SMTP server receives nothing.
- **Send only drafts (0.4):** `send_draft` with a UID outside `Drafts` is refused.

## Live tests

They run the real tools end to end over MCP against your own Bridge, and change nothing in the mailbox:

- the `check` steps;
- the folder roles;
- newest-first by date;
- reading an unread message leaves it unread, and a stale `uidvalidity` is refused;
- every search criterion is accepted;
- the largest folder lists well within the operation timeout.
- sender, subject and body searches in the largest folder, timed with a 5 min limit so the real time shows (issue #39).

```powershell
go test -tags live ./internal/server -run Live -v
```

They use what `proton-mail-mcp setup` saved (settings, and the password from the credential store), or a `.env` named by `PROTON_MCP_ENV_FILE`. Without either they skip. The output holds no subjects, senders or bodies, but look before pasting it into an issue: the repository is public.

Run them after a Bridge update, and before a release.
## Conventions

- Tests sit next to the code: `foo.go` → `foo_test.go`, same package unless the test is about the public API.
- Test data in `testdata/` next to the test (Go ignores the directory in builds). The repo is public: test messages are invented, with addresses on the reserved `example.com` / `example.org` domains. A real-world `.eml` that reproduces a bug is rewritten (names, addresses, subject, body, `Message-ID`, `Received` headers) before it is committed.
- Helpers that start the in-memory servers live in `internal/testutil/`.
- `t.Parallel()` where tests don't share state.
- Golden files are updated with `-update`, never by hand.
- No sleeps to wait for something: use channels, contexts, or the servers' own callbacks.

## Running

```powershell
./tools/task.ps1 test            # all tests
./tools/task.ps1 test -Race      # with the race detector (needs cgo: CI on Linux)
go test ./internal/mail -run TestFetch -v
go test ./internal/mail -fuzz FuzzParseMessage -fuzztime 60s
go test -tags live ./internal/server -run Live -v   # against a real Bridge, after setup
```

## Coverage

CI reports coverage but sets no threshold: a percentage says little about whether the right things are tested. What is required is that the guardrails have tests, and that a bug fix comes with a test that failed before the fix.
