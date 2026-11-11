# proton-mail-mcp

An MCP server that lets Claude manage a Proton Mail inbox through
[Proton Mail Bridge](https://proton.me/mail/bridge) (local IMAP/SMTP).

```
Claude ──MCP──► proton-mail-mcp ──IMAP/SMTP (127.0.0.1)──► Proton Mail Bridge ──► Proton
```

## Design principles

- **Everything runs locally.** Bridge decrypts mail on your machine; this server talks to Bridge over localhost.
- **Claude may read, search, organize and write drafts.**
- **Claude may only send on your command.** Sending is:
  1. off unless `PROTON_ALLOW_SEND=1`,
  2. limited to sending an *existing draft* (`send_draft`), and
  3. set to "always ask" in Claude, so the app asks you before every send.
- **No permanent deletion.** "Delete" means move to Trash.
- **Email content is untrusted.** Mails can contain text meant to steer the AI (prompt injection); the server marks message content as untrusted and the send guard above is the hard stop.

## Requirements

- Proton plan that includes Bridge (Mail Plus, Unlimited, …)
- Proton Mail Bridge installed and signed in
- Python 3.11+

## Setup

```powershell
python -m venv .venv
.\.venv\Scripts\python.exe -m pip install -e ".[dev]"
copy .env.example .env   # then fill in Bridge user + password
```

Run tests:

```powershell
.\.venv\Scripts\python.exe -m pytest
```

## Connecting to Claude

_Added when the tools are implemented._
