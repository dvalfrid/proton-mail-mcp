# Roadmap

Each milestone is one release. Later milestones widen what Claude may do, so each one starts only when the previous is in daily use and trusted.

## 0.1 — Foundation and read-only

Part 1 (#3):

- ✅ `internal/config`: load `.env` / environment, validate (local host only, port ranges), password as a never-printing `Secret`.
- ✅ `internal/mail`: IMAP client over STARTTLS to Bridge, per-operation timeout and cancellation, one connection with a mutex, reconnect on drop, read-only (EXAMINE).
- ✅ `internal/testutil`: in-memory IMAP server with STARTTLS.
- ✅ Tools: `list_folders`, `list_messages` (folder, limit, unread only).
- ✅ Guardrail tests: tool list snapshot, read-only annotations, no send tools, never marks read, no secrets or mail content in output/logs, local only.
- ✅ README: setup and connecting to Claude Desktop and Claude Code.

Part 2:

- ✅ Message parsing (#12): HTML → text, attachments listed only, size caps, fuzz test.
- ✅ Tool: `read_message` (#13), message returned in an untrusted-content block.
- ✅ Tool: `search_messages` (#14).
- Order `list_messages` and `search_messages` by date, not uid: Bridge's uids don't follow dates.

## 0.2 — Organize

- Tools: `move_message`, `mark_read` / `mark_unread`, `flag` / `unflag`, `trash_message` (move to Trash).
- Guardrail test: no `EXPUNGE` in any scenario.

## 0.3 — Drafts

- Tools: `create_draft`, `create_reply_draft` (threading headers: `In-Reply-To`, `References`), `update_draft`, `list_drafts`.

## 0.4 — Send on command

- `internal/send`: SMTP over STARTTLS; the only package allowed to send.
- Tool: `send_draft` — sends an existing draft, refused unless `PROTON_ALLOW_SEND=1`, moves the draft away after sending.
- Guardrail tests: refused without the flag, refuses non-drafts, nothing else reaches SMTP.
- README: configuring the client to always ask before `send_draft`.

## Installation

Part of a working product: a user sets it up without editing files by hand, on Windows, macOS and Linux. Design and research: [docs/installation-research.md](docs/installation-research.md).

- `proton-mail-mcp setup` (#30):
  - detect Bridge;
  - pin its certificate (no export);
  - ask for the Bridge password with hidden input, and keep it in the OS credential store;
  - register the server in Claude.
- `proton-mail-mcp check` and clear startup errors (#31).

## Distribution

Only once the product works (the milestones above).

- Signed Windows binary (Azure Artifact Signing) and a winget package (#32).
- Homebrew tap, Linux `.deb`/`.rpm`, a Claude Desktop extension (MCPB) (#33).

## Later (ideas, not commitments)

- Labels (Proton's `Labels/` folders).
- Attachment metadata and saving an attachment to a chosen folder.
- Optional direct Proton API (`go-proton-api`) instead of Bridge — unofficial API, its own risk assessment first.
