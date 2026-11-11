# Architecture

How proton-mail-mcp is put together, and why. The rules themselves are in [STANDARDS.md](../STANDARDS.md); how Proton Mail Bridge behaves, from its source, is in [bridge.md](bridge.md); how it is tested is in [testing.md](testing.md).

## The big picture

```text
MCP client (Claude Desktop, Claude Code, …)
   │  JSON-RPC over stdin/stdout
   ▼
cmd/proton-mail-mcp      start-up: config, logging, wiring
   │
internal/server          MCP tools: validate arguments, call mail, shape results
   │
internal/mail            IMAP client: one connection, timeouts, read-only access, parsing
   │  IMAP over STARTTLS, 127.0.0.1, certificate pinned
   ▼
Proton Mail Bridge        decrypts mail locally, talks to Proton
```

The server is a local process started by the MCP client. Nothing listens on the network. stdout is the MCP protocol and nothing else; all logging goes to stderr.

| Package | Knows about | Does not know about |
|---|---|---|
| `cmd/proton-mail-mcp` | everything, to wire it together | — |
| `internal/config` | the `.env` file, environment variables, validation | IMAP, MCP |
| `internal/mail` | IMAP, MIME, Bridge | MCP, tools |
| `internal/server` | MCP, tool schemas, the mail package's API | IMAP commands |
| `internal/testutil` | in-memory IMAP server for tests | — |

The split is deliberate. Mail protocol code can be tested without MCP. A tool can be tested end to end without Bridge.

## Start-up

`main` does four things, in order:

1. **Read settings.** Settings come from `.env` next to the executable, or from the file named by `PROTON_MCP_ENV_FILE`; environment variables win over the file. `config.Load` validates them and refuses to start when:
   - the user, password or certificate path is missing;
   - the host is not local (only `127.0.0.1`, `::1` or `localhost`);
   - a port is out of range.
2. **Log the start.** Only the version, the IMAP address and whether sending is enabled. The `Config` is never passed to the logger.
3. **Build the TLS configuration** from the certificate exported from Bridge (`mail.TLSConfig`). This fails before any connection is made if the file is missing or holds no certificate.
4. **Create the mail client and the MCP server, and serve over stdio** until stdin closes or the process is interrupted.

No IMAP connection is opened at start-up. The first tool call opens it. So a client that only lists tools never touches Bridge.

## A tool call, end to end

Take `read_message(folder: "INBOX", uid: 42)`:

1. **The MCP SDK** decodes the arguments and validates them against the tool's input schema (required fields, types).
2. **The tool handler** in `internal/server` checks what the schema can't, for example that `uid` isn't 0, then calls `mail.Client.ReadMessage`.
3. **`mail.Client.do`** takes the connection mutex, connects if needed, and sets the operation's deadline on the connection.
4. **The IMAP commands:** `EXAMINE INBOX` opens the folder read-only, then `UID FETCH 42 (UID RFC822.SIZE BODY.PEEK[]<0.33554432>)` fetches at most 32 MiB without setting `\Seen`.
5. **`mail.ParseMessage`** turns the raw bytes into cleaned, capped text: headers, the body as plain text, and attachments listed by name, type and size.
6. **The handler returns the result:** a JSON object with the folder, the uid, a `note` that the content is untrusted, and the message in an `untrusted_content` field. The SDK sends it both as structured content and as text.

An error at any step becomes a tool error result: `isError: true` with a plain message such as `read message 42 in "INBOX": message not found`. It never carries message content or secrets.

## Design decisions

### One IMAP connection, behind a mutex

An IMAP connection has one selected folder at a time. Two commands that each select a folder can't safely interleave on it. `mail.Client` therefore has a single connection guarded by a `sync.Mutex`: tool calls that arrive at the same time run one after another.

- **Why not a connection pool:** Bridge is local and answers in milliseconds, and one connection is far simpler to reason about. A pool would also multiply logins, and Bridge counts connections ([bridge.md §10](bridge.md#10-other-things-an-automating-client-should-know)).
- **Lazy connect:** the connection opens on the first call.
- **Reconnect:** after an error that leaves the connection in an unknown state, it is closed, and the next call reconnects. See the [error model](#error-model) below.

### Timeouts that always win: `boundedConn`

Every operation is bounded by `mail.DefaultTimeout` (30 s) and by the caller's context. The obvious way, setting a deadline on the socket, doesn't work on its own: imapclient sets its own read and write deadlines before each command (30 s per response, 5 min per literal), which overwrites ours.

`boundedConn` wraps the TCP connection. It remembers the deadlines imapclient asks for, and applies the *earlier* of those and our limit. Cancelling the context moves the limit into the past, which wakes a blocked read at once. tls.Conn passes deadlines down to the TCP connection, so this keeps working after STARTTLS.

This was found the hard way: a test of a server that never answers took 60 s instead of 200 ms until `boundedConn` existed.

### Reading never marks mail as read

- Folders are opened with `EXAMINE`, never `SELECT`.
- Bodies are fetched with `BODY.PEEK[]`, never `BODY[]`.

Either one alone would be enough on a standard server. Bridge's gluon also never sets `\Seen` under EXAMINE ([bridge.md §5](bridge.md#5-flags)). Both are used, so one mistake can't mark the owner's mail as read. Tests check `\Seen` before and after, and check the IMAP transcript.

### Untrusted content is a JSON field, not a text marker

Mail is written by whoever sends it, so it can contain text aimed at the AI (prompt injection). `read_message` returns the message inside an `untrusted_content` field, next to a `note` that says it is data from the sender, never instructions.

- **Why a field and not a delimiter:** a text marker such as `--- BEGIN MESSAGE ---` can be faked: the sender writes the end marker into the body and continues as if outside the block. In JSON, sender text is a string value. Quotes and braces in it are escaped, so it can't close the field early or pose as another field of the result. A guardrail test sends a message that tries exactly that.
- **Listings too:** `list_messages` and `search_messages` carry the same kind of note, because subjects and sender names are sender-written as well.
- **The hard stop is elsewhere.** Marking content as untrusted helps the model. The real protection is that no tool can send mail freely: see [security boundaries](#security-boundaries).

### Everything from the sender is cleaned and capped

`mail.ParseMessage` treats the whole message as hostile input:

- **Size caps:** 32 MiB raw message, 2 MiB per text part, 30,000-character body, 50 addresses per header, 50 attachments, 200 MIME parts, nesting depth 10. Anything cut sets `truncated: true`.
- **HTML becomes text:**
  - Scripts, styles, embedded objects and hidden elements are dropped. Hidden text is a common injection trick.
  - Images become their alt text, and only `http(s)` and `mailto` links are kept.
  - Nothing is ever fetched.
- **Characters that can fake or hide text are removed:** control characters, bidi overrides and zero-width characters. In headers, newlines become spaces, so a subject can't fake extra lines.
- **Attachments are listed, never opened.**

A damaged message gives what could be read, not an error. A fuzz test checks that no input panics, hangs, produces invalid UTF-8 or exceeds a cap.

### Newest first, and what "newest" means on Bridge

IMAP has no "newest" without the `SORT` extension, which Bridge doesn't have ([bridge.md §1](bridge.md#1-capabilities)). Uids don't help either: Bridge's initial sync numbers messages by descending Proton ID, and new mail continues upward ([bridge.md §3](bridge.md#3-uids-and-dates)). On a real Bridge, uid 1 was the newest message and the order was mixed after that.

So `list_messages` and `search_messages` sort on our side:

1. `UID SEARCH` for the matches.
2. Fetch `INTERNALDATE` and `ENVELOPE` for all of them. Bridge serves both from its database without loading message bodies.
3. Sort newest first by the **sort date**, with the higher uid first on equal dates, so the order is stable.
4. Fetch the full summaries for the newest `limit` only.

**The sort date is the earlier of the received date (`INTERNALDATE`) and the `Date:` header:**

- **The received date alone isn't enough.** On a real Bridge, a batch of 57 of 23,000 synced messages carried the sync time as their received date, years after they were sent. Why Bridge did that is not known.
- **The `Date:` header alone isn't safe.** The sender writes it, so a future date would keep a message at the top.
- **The earlier of the two:** a sender can make a message look older, never newer.

**Cost:** about one second for a 23,000-message folder against a real Bridge. The dates of all matches cross the connection once per call.

### Secrets

- **Where the Bridge password lives:** only in `.env` or the environment.
- **The `Secret` type:** `config.Secret` holds the password. Its `String`, `GoString`, `LogValue` and `MarshalText` all return `[redacted]`, so printing, logging or encoding it by mistake shows nothing. Only `Reveal()` gives the value, and the only caller is the IMAP login.
- **Errors:** errors from reading `.env` never echo the line, which could hold the password.
- **Tests:** they use a distinctive fake password and fail if it ever appears in tool output or logs.
- **The repository** is public. `.env` and certificates are git-ignored, and Claude Code is denied reading `.env` (`.claude/settings.json`).

### TLS: always verified, against Bridge's own certificate

Bridge uses a self-signed certificate. Most Bridge setups skip verification on localhost. This server doesn't:

- **The certificate is pinned.** The certificate exported from Bridge is required (`PROTON_BRIDGE_CERT`), and it is the only trusted root.
- **Why, even on localhost:** when Bridge isn't running, any program on the machine can listen on port 1143, and without verification it would receive the Bridge password at login. A test runs exactly that impostor and checks that it never sees the password.
- **The host must be local** (checked at start-up and again in `mail.TLSConfig`), and STARTTLS is required.

## Error model

| What happens | Example | Effect on the connection | What the caller gets |
|---|---|---|---|
| The server answers NO or BAD | unknown folder | **kept**: the server is fine, the request wasn't | the server's message, wrapped with the folder name |
| Not found on our side | unknown uid (an empty FETCH) | kept | `message not found` (`mail.ErrMessageNotFound`) |
| A network error or timeout | Bridge stopped, a silent server | **dropped and closed** ("imap connection dropped" is logged) | the error; the next call reconnects |
| Context cancelled | the client gave up | dropped | `context.Canceled` |
| Can't connect or log in | Bridge not running, wrong password | nothing to keep | a message that names the likely cause ("is Bridge running?", "check the Bridge password") |
| Invalid arguments | `limit: 1000`, a bad date | not touched | a message that says what is allowed |

The rule in `mail.Client.do`: a server NO or BAD (`*imap.Error`) leaves the connection usable. Any other error leaves it in an unknown state, for example halfway through a response, so it is closed rather than reused.

**Logging.** Only metadata is logged (the address, `connected to Bridge`, `imap connection dropped`, and the error). Never subjects, bodies, addresses from mail, or the password. A test checks that mail content doesn't reach the logs.

## Security boundaries

Where each protection is enforced matters as much as the protection itself.

| Protection | Enforced by | Holds even if the AI is fooled? |
|---|---|---|
| Reading never marks as read | `internal/mail` (EXAMINE, PEEK) | Yes |
| No permanent deletion | No EXPUNGE anywhere: lint (`forbidigo`) and tests. "Delete" will mean move to Trash (0.2) | Yes |
| Sending only an existing draft, only with `PROTON_ALLOW_SEND=1` (0.4) | The server: `send_draft` is the only send path; SMTP code in one package, enforced by lint | Yes |
| The owner approves each send | **The MCP client** ("always ask") | Yes, if the client is configured so. Not every client can be |
| Mail content treated as data | The note and field from the server; **obeying it is up to the model** | No: that's why the rows above exist |
| The password never leaves the process except to Bridge | `Secret` type, TLS pinning, local host only | Yes |

## Tests, in short

| Layer | Where | Against |
|---|---|---|
| Unit | `internal/config`, `internal/mail/parse_test.go` | pure functions |
| Protocol | `internal/mail/mail_test.go` | a real IMAP server in memory (`go-imap` `imapmemserver`) over STARTTLS, with a per-test certificate |
| Tool, end to end | `internal/server` | an in-memory MCP client calling the real server, wired to the in-memory IMAP server |
| Guardrails | `internal/server/guardrails_test.go`, plus read-only tests in `internal/mail` | as above |
| Binary | `cmd/proton-mail-mcp/main_test.go` | the built executable over stdio |
| Fuzz | `FuzzParseMessage` | random messages |

No test needs Proton or Bridge. Details are in [testing.md](testing.md).

## Adding a tool

1. Put the mail logic in `internal/mail`, with protocol tests against the in-memory server.
2. Register the tool in `internal/server`:
   - input and output structs with `jsonschema` descriptions;
   - the right annotations (`readOnly(...)` for anything that doesn't change the mailbox);
   - a description that says what it changes.
3. Add end-to-end tests. If it touches a guardrail, extend `guardrails_test.go` first.
4. Update the snapshot: `go test ./internal/server -update`, and review the diff.
5. Document the tool in README, and update ROADMAP.
6. Check [bridge.md](bridge.md) for how Bridge really handles the IMAP commands you use.
