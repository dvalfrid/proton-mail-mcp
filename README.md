<p align="center"><img src="assets/png/icon-128.png" width="96" height="96" alt=""></p>

# proton-mail-mcp

An MCP server that lets Claude manage a Proton Mail inbox through
[Proton Mail Bridge](https://proton.me/mail/bridge) (local IMAP/SMTP).

Website: [proton-mail-mcp.codeby.se](https://proton-mail-mcp.codeby.se)

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

Claude can list, search and read messages, mark them read or unread, star them, move and label them, move them to Trash, and write new drafts. Reply drafts and sending follow: see [ROADMAP.md](ROADMAP.md).

"Newest first" in `list_messages` and `search_messages` means by date. Proton Mail Bridge doesn't number messages by date and has no IMAP `SORT`, so the server sorts itself ([docs/architecture.md](docs/architecture.md#newest-first-and-what-newest-means-on-bridge)). In a large folder this takes about a second.

## Tools

| Tool | What it does | Changes the mailbox |
|---|---|---|
| `list_folders` | Folders with message and unread counts | No |
| `list_messages` | Newest messages in a folder: uid, date, sender, subject, read/flagged, plus the folder's `uidvalidity` | No (messages stay unread) |
| `search_messages` | Messages in one folder matching all given criteria: `from`, `to`, `subject`, `body` (case-insensitive text), `since` / `before` (dates received), `unread_only`. At least one criterion; same result shape as `list_messages` | No (messages stay unread) |
| `read_message` | One message by folder and uid (optionally with the `uidvalidity` the uid came with, so a folder that Bridge renumbered is refused rather than another message read): headers, body as plain text, attachments listed (index, name, type, size). Returned in an `untrusted_content` field with a note that it is data from the sender, not instructions. HTML is converted to text and nothing is fetched; attachments are never opened. Long messages are cut and marked `truncated` | No (the message stays unread) |
| `view_image` | One image from a message, by its `index` in `read_message`'s attachment list, as image content Claude can see: an attached image or one embedded in the HTML (inline). PNG, JPEG, GIF (first frame) and WebP, checked by content, not by the declared type; decoded and encoded again, and scaled down to 1568 pixels on the longest side. Images larger than 20 MiB or 50 megapixels are refused. Images the message links to on the internet are never fetched. Marked as untrusted, like the message | No (the message stays unread) |
| `save_attachment` | Save one attachment, by its `index` in `read_message`'s list, into the attachment folder (`PROTON_ATTACHMENT_DIR`), so you can open it. Claude can't choose the folder. The sender's file name is cleaned (no path, no reserved or hidden characters), an existing file is never replaced (a number is added), and the file is never opened. On Windows it gets the Mark of the Web, like a browser download, so Windows and Office warn before running or editing it. Needs the `uidvalidity`; at most 30 MiB | No (the message stays unread); writes one new file |
| `mark_read` / `mark_unread` | Mark messages read or unread. Synced to Proton | Yes (undoable) |
| `flag` / `unflag` | Star or unstar messages (Proton's Starred) | Yes (undoable) |
| `move_message` | Move messages to INBOX, Archive or a folder under `Folders/` (also out of Spam). Not into Spam, to Trash (`trash_message`) or a label (`add_label`); not out of a label, Drafts or All Mail | Yes (undoable) |
| `add_label` | Add a label (`Labels/…`); the messages stay in their folder | Yes (undoable) |
| `trash_message` | Move messages to Trash. Nothing is deleted; they can be restored until Trash is emptied. Annotated destructive, so clients that honour it ask first. Not for drafts | Yes (undoable) |
| `create_draft` | Write a new plain-text email (`to`, `cc`, `bcc`, `subject`, `body`) as a draft in Drafts, from your Proton address. Nothing is sent: you read, edit and send it in Proton Mail. Counts toward `PROTON_MAX_CHANGES` | Adds a draft |

The tools that change messages take a folder, 1 to 50 `uids` from that folder and the `uidvalidity` that came with them (required). If the folder was renumbered or any uid is missing, the call changes nothing. No tool ever deletes permanently.

## Requirements

- Proton plan that includes Bridge (Mail Plus, Unlimited, …)
- Proton Mail Bridge installed and signed in
- To use: a release binary (single file, no runtime dependencies)
- To build: Go (version in [`go.mod`](go.mod)) and PowerShell 7

## Setup

**On Windows, the easy way:** download `proton-mail-mcp_<version>_windows_amd64_setup.exe` (or `_arm64_` for Windows on ARM) from the release and run it. It installs for you only, without administrator rights, into `%LOCALAPPDATA%\Programs\proton-mail-mcp`, adds *Proton Mail MCP Setup* and *Proton Mail MCP Check* to the Start menu, and starts the setup wizard below. Running a newer installer later only updates the program: no questions, your settings stay. Uninstall from *Settings → Apps*; it removes the program from Claude too, and your settings and saved password only if you tick that box.

**On Windows** there is also a graphical setup: `proton-mail-mcp-setup.exe`, next to `proton-mail-mcp.exe` in the Windows download. Start it (double-click) and it does the steps below in a window that follows the Windows 11 look, light or dark: no terminal needed. It needs Microsoft Edge WebView2, which Windows 11 has; without it, it opens the terminal setup instead. macOS and Linux get it later ([#91](https://github.com/dvalfrid/proton-mail-mcp/issues/91)).

Put the program somewhere permanent, for example `C:\Tools\proton-mail-mcp\` (Windows) or `~/bin` (macOS/Linux). Build it with `./tools/task.ps1 build`, or download a release. Then, with Proton Mail Bridge running and signed in, run in a terminal:

```powershell
proton-mail-mcp setup
```

It works the same on Windows, macOS and Linux:

1. **Finds Bridge.** It tells you whether Bridge is not installed, not running, or on another port.
2. **Pins Bridge's certificate.** It reads the certificate over STARTTLS, without trusting it, and shows its fingerprint. On Windows it pins straight away when the port belongs to Proton's validly signed `bridge.exe`; otherwise it asks you to confirm that Bridge is running. No export is needed.
3. **Asks for the Bridge username and Bridge password** (Bridge → your account → Mailbox details; this is not your Proton password). The password is typed in the terminal, hidden, and never passes through Claude.
4. **Tests the login**, then saves: the password in the OS credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service), and the other settings plus the pinned certificate in your config directory (`%APPDATA%\proton-mail-mcp`, `~/Library/Application Support/proton-mail-mcp`, or `~/.config/proton-mail-mcp`), readable only by you.
5. **Offers to register the server** in Claude Code (`claude mcp add --scope user`) and Claude Desktop. It merges into Claude Desktop's config with a backup, and never puts the password there.
6. **Runs `check`.**

Bridge on another port: `proton-mail-mcp setup -imap-port 1144 -smtp-port 1026`. Nothing is saved unless the login works. Run setup again after you reset or reinstall Bridge.

### Settings by hand

Instead of `setup`, settings can come from environment variables, or from a `.env` file next to the program or named by `PROTON_MCP_ENV_FILE`. The environment wins over `.env`, and `.env` over what setup saved. The password comes from the credential store only if it is in neither.

For the certificate, export it from Bridge (Settings → Advanced → Export TLS certificates). Keep `cert.pem`, and **delete `key.pem` straight away**: the export also writes Bridge's private TLS key, which nothing here needs and which would let a program pose as Bridge.

| Setting | Default | |
|---|---|---|
| `PROTON_BRIDGE_USER` | — | Bridge username (your address) |
| `PROTON_BRIDGE_PASSWORD` | — | Bridge password |
| `PROTON_BRIDGE_HOST` | `127.0.0.1` | Only `127.0.0.1`, `::1` or `localhost` are accepted |
| `PROTON_BRIDGE_IMAP_PORT` | `1143` | |
| `PROTON_BRIDGE_SMTP_PORT` | `1025` | |
| `PROTON_BRIDGE_CERT` | — | Path to Bridge's certificate (PEM). Required |
| `PROTON_ALLOW_SEND` | `0` | Sending is not implemented yet |
| `PROTON_ATTACHMENT_DIR` | `Proton Mail MCP` in your Downloads folder | The only folder `save_attachment` saves into, as a full path. Created on the first save |
| `PROTON_MAX_CHANGES` | `200` | How many messages the organize tools may change, and drafts `create_draft` may write, together, per server run: a guard against instructions hidden in emails. Restart Claude to reset the count |
| `PROTON_MCP_CONFIG_DIR` | your config dir | Where setup's settings are kept |
Check the program: `proton-mail-mcp --version`.

Check the whole connection: `proton-mail-mcp check`. It goes through the settings, whether Bridge answers and is really Bridge, the pinned certificate, the login, SMTP, (on Windows) which program owns the port, and whether a Claude client has the server registered. Each line says what is wrong and how to fix it; the exit code is non-zero on failure. It reads no mail and prints no secrets. The server also logs a warning at start-up if Bridge isn't ready.

## Troubleshooting

Run `proton-mail-mcp check` first: it tests every step and says what to do. Errors Claude shows end with a Help link to [docs/troubleshooting.md](docs/troubleshooting.md), which explains each one, where the logs are and how to report a problem.

## Development

```powershell
./tools/task.ps1 setup      # git hooks, first time
./tools/task.ps1 verify     # tidy, format, lint, tests, vulnerability scan
```

How to contribute, commit format, CI and releases: [CONTRIBUTING.md](CONTRIBUTING.md). Test strategy: [docs/testing.md](docs/testing.md). How it is built and why: [docs/architecture.md](docs/architecture.md).

## Connecting to Claude

`proton-mail-mcp setup` does this for you. By hand:

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
