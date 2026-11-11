# Testing

How this project is tested, and why. Rules in short: [STANDARDS.md](../STANDARDS.md#testing).

## Goal

Every change can be verified on any machine, and in CI, **without Proton, without Bridge and without a real mailbox**. A real Bridge is used only for a final manual check of a feature.

## Layers

| Layer | What it checks | How | Runs in |
|---|---|---|---|
| Unit | Pure logic: config parsing, MIME → text, output capping, guard decisions | Table-driven `go test` | CI, every push |
| Protocol | IMAP/SMTP code against a real protocol implementation | In-memory servers: `go-imap/v2/imapserver` + `imapmemserver`, `go-smtp` server | CI, every push |
| Tool (end to end) | A tool as Claude sees it: arguments in, result out | In-memory MCP client ↔ server (`mcp.NewInMemoryTransports`) wired to the in-memory mail servers | CI, every push |
| Guardrails | The [security guardrails](../STANDARDS.md#security-guardrails) hold | Dedicated tests, see below | CI, every push |
| Fuzz | Parsing untrusted mail never panics or hangs, output stays capped | `go test -fuzz` targets; seed corpus in `testdata/fuzz/` | CI runs the seed corpus; longer runs by hand |
| Live | The real thing: Bridge + Proton | `go test -tags live ./...` with `.env`, against a dedicated test folder | By hand only |

## Guardrail tests

Each hard rule has a test that fails if the rule breaks. They live in `internal/server/guardrails_test.go` (added with the tools):

- **Tool list snapshot:** the exact set of tool names and their input schemas is compared with a golden file (`testdata/tools.golden.json`). Adding or changing a tool is a visible diff in review. Update with `go test ./internal/server -update`.
- **No free-form send:** no tool other than `send_draft` reaches the SMTP server; `send_draft` without `PROTON_ALLOW_SEND=1` returns an error and the fake SMTP server receives nothing.
- **Send only drafts:** `send_draft` with a UID outside `Drafts` is refused.
- **No expunge:** the fake IMAP server records every command; no test scenario may produce `EXPUNGE` or `UID EXPUNGE`. "Delete" leaves the message in `Trash`.
- **Untrusted content is marked:** a message containing instructions ("ignore previous instructions…") comes back inside the untrusted-content block.
- **No secrets in output or logs:** the test password never appears in tool results, errors, or captured log output.
- **Local only:** a non-local Bridge host is refused at start-up.

## Conventions

- Tests sit next to the code: `foo.go` → `foo_test.go`, same package unless the test is about the public API.
- Test data in `testdata/` next to the test (Go ignores the directory in builds). Real-world `.eml` samples are anonymised before they are committed.
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
go test -tags live ./...         # against a real Bridge, needs .env
```

## Coverage

CI reports coverage but sets no threshold: a percentage says little about whether the right things are tested. What is required is that the guardrails have tests, and that a bug fix comes with a test that failed before the fix.
