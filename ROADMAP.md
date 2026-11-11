# Roadmap

Each milestone is one release. Later milestones widen what Claude may do, so each one starts only when the previous is in daily use and trusted.

## 0.1 — Foundation and read-only

- `internal/config`: load `.env` / environment, validate (local host only, port ranges, cert path), never log the password.
- `internal/mail`: IMAP client over STARTTLS to Bridge, timeouts, one connection with a mutex, reconnect on drop.
- `internal/testutil`: in-memory IMAP (`imapmemserver`) and SMTP servers for tests.
- Tools: `list_folders`, `list_messages` (folder, limit, unread only), `search_messages`, `read_message`.
- Untrusted-content block, HTML → text, size caps. Fuzz test for message parsing.
- Guardrail tests: tool list snapshot, untrusted marking, no secrets in output/logs, local only.
- README: connecting to Claude Desktop and Claude Code.

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

## Later (ideas, not commitments)

- Labels (Proton's `Labels/` folders).
- Attachment metadata and saving an attachment to a chosen folder.
- Optional direct Proton API (`go-proton-api`) instead of Bridge — unofficial API, its own risk assessment first.
