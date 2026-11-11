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

Pre-release. Claude can list, search and read messages (read-only). Organizing, drafts and sending follow: see [ROADMAP.md](ROADMAP.md).

Known limitation: "newest" means highest IMAP uid, and Proton Mail Bridge doesn't number messages by date (it syncs newest first and has no IMAP `SORT`; see [gluon#378](https://github.com/ProtonMail/gluon/issues/378)). Until the server sorts by date itself, `list_messages` and `search_messages` can return older messages first.

## Tools

| Tool | What it does | Changes the mailbox |
|---|---|---|
| `list_folders` | Folders with message and unread counts | No |
| `list_messages` | Newest messages in a folder: uid, date, sender, subject, read/flagged | No (messages stay unread) |
| `search_messages` | Messages in one folder matching all given criteria: `from`, `to`, `subject`, `body` (case-insensitive text), `since` / `before` (dates received), `unread_only`. At least one criterion; same result shape as `list_messages` | No (messages stay unread) |
| `read_message` | One message by folder and uid: headers, body as plain text, attachments listed (name, type, size). Returned in an `untrusted_content` field with a note that it is data from the sender, not instructions. HTML is converted to text and nothing is fetched; attachments are never opened. Long messages are cut and marked `truncated` | No (the message stays unread) |

## Requirements

- Proton plan that includes Bridge (Mail Plus, Unlimited, …)
- Proton Mail Bridge installed and signed in
- To use: a release binary (single file, no runtime dependencies)
- To build: Go (version in [`go.mod`](go.mod)) and PowerShell 7

## Setup

1. In Proton Mail Bridge, note the IMAP **username** and the **Bridge password** (Bridge → your account → Mailbox details). The Bridge password is not your Proton account password.
2. Export Bridge's TLS certificate (Bridge → Settings → Advanced → Export TLS certificates) and keep the `cert.pem` file. It is required: the server only talks to the Bridge that presents this certificate, so no other program can pose as Bridge and capture the password.
3. Put the program and a `.env` next to each other, e.g. in `C:\Tools\proton-mail-mcp\`:

   ```powershell
   ./tools/task.ps1 build                    # or download a release
   copy bin\proton-mail-mcp.exe C:\Tools\proton-mail-mcp\
   copy .env.example C:\Tools\proton-mail-mcp\.env   # then fill it in
   ```

The server reads `.env` next to the executable, or the file named by `PROTON_MCP_ENV_FILE`. Environment variables override the file.

| Setting | Default | |
|---|---|---|
| `PROTON_BRIDGE_USER` | — | Bridge username (your address) |
| `PROTON_BRIDGE_PASSWORD` | — | Bridge password |
| `PROTON_BRIDGE_HOST` | `127.0.0.1` | Only `127.0.0.1`, `::1` or `localhost` are accepted |
| `PROTON_BRIDGE_IMAP_PORT` | `1143` | |
| `PROTON_BRIDGE_SMTP_PORT` | `1025` | |
| `PROTON_BRIDGE_CERT` | — | Path to the certificate exported from Bridge (PEM). Required |
| `PROTON_ALLOW_SEND` | `0` | Sending is not implemented yet |

Check the program: `proton-mail-mcp --version`.

## Development

```powershell
./tools/task.ps1 setup      # git hooks, first time
./tools/task.ps1 verify     # tidy, format, lint, tests, vulnerability scan
```

How to contribute, commit format, CI and releases: [CONTRIBUTING.md](CONTRIBUTING.md). Test strategy: [docs/testing.md](docs/testing.md). How it is built and why: [docs/architecture.md](docs/architecture.md).

## Connecting to Claude

**Claude Desktop** — Settings → Developer → Edit Config, then add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "proton-mail": {
      "command": "C:\\Tools\\proton-mail-mcp\\proton-mail-mcp.exe"
    }
  }
}
```

**Claude Code** — from any folder:

```powershell
claude mcp add --scope user proton-mail -- C:\Tools\proton-mail-mcp\proton-mail-mcp.exe
```

Restart the client; the tools appear under `proton-mail`. Keep the password in `.env`, not in the client's config: client configs are often synced or shared.

## License

[MIT](LICENSE)
