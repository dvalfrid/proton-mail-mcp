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

Full rules and threat model: [STANDARDS.md](STANDARDS.md#security-guardrails), [SECURITY.md](SECURITY.md).

## Status

Pre-release: the project foundation is in place, the mail tools are not yet. See [ROADMAP.md](ROADMAP.md).

## Requirements

- Proton plan that includes Bridge (Mail Plus, Unlimited, …)
- Proton Mail Bridge installed and signed in
- To use: a release binary (single file, no runtime dependencies)
- To build: Go (version in [`go.mod`](go.mod)) and PowerShell 7

## Setup

```powershell
copy .env.example .env      # then fill in Bridge user + password
./tools/task.ps1 build      # → bin/proton-mail-mcp.exe
```

## Development

```powershell
./tools/task.ps1 setup      # git hooks, first time
./tools/task.ps1 verify     # tidy, format, lint, tests, vulnerability scan
```

How to contribute, commit format, CI and releases: [CONTRIBUTING.md](CONTRIBUTING.md). Test strategy: [docs/testing.md](docs/testing.md).

## Connecting to Claude

_Added with the first tools (milestone 0.1)._

## License

[MIT](LICENSE)
