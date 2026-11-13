# Roadmap

Each milestone is one release and has a GitHub milestone with the same name holding its issues. Later milestones widen what Claude may do, so each one starts only when the previous is in daily use and trusted.

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
- ✅ Order `list_messages` and `search_messages` by date, not uid (#18): Bridge's uids don't follow dates.

## 0.2 — Organize and hardening

Organize:

- ✅ Tools: `mark_read` / `mark_unread`, `flag` / `unflag` (#51).
- ✅ Tools: `move_message`, `add_label` (#52). Not into Spam (#74).
- ✅ Tool: `trash_message`, move to Trash, annotated destructive (#53).
- ✅ Every tool that changes a message requires the `uidvalidity` its uid came with (#25), so a renumbered folder can't make it act on another message.
- ✅ At most `PROTON_MAX_CHANGES` messages (200 by default) changed per server run (#68).
- ✅ Guardrail tests: no `EXPUNGE` or `\Deleted` from any tool; every tool classified.
- ✅ Live test of the organize tools on one marked test message (#54).

Hardening:

- ✅ A panic fails one call, not the server (#60).
- ✅ Error messages say what to do and link to [docs/troubleshooting.md](docs/troubleshooting.md); the server starts without settings and says to run setup (#62).
- ✅ Clear error when Bridge can't reach Proton, and that a change may be partly done (#64).
- ✅ Text search gets its own timeout and advice (#39, shipped in 0.1.0).
- ✅ Weekly fuzzing in CI (#65).
- ✅ Independent security review (#67) and its fixes: setup trusts only a signed `bridge.exe` in Program Files (#71), no slow parse on crafted HTML (#72), no sender text in errors (#73), the low and informational findings (#74, #75).
- ✅ Release tooling: `vX.Y.Z` tags, complete Windows zips (#47, #49).
- ✅ Windows binaries signed with Azure Artifact Signing (0.2.0).

## 0.3 — Windows installer and winget

Install without a terminal, and updates that keep Claude's registration:

- ✅ Graphical setup wizard on Windows (#90): Windows 11 look, light and dark, the saved password reused; no white flash (#94), no stray terminal window (#96).
- ✅ Installer (#89): per user, fixed folder, setup on first install, silent updates, uninstall that removes the program from Claude.
- ✅ Setup switches a registration of another copy to the installed one (#103).
- ✅ Project icon in the exes, the wizard, the installer and `assets/` (#98).
- A winget package, `Codeby.ProtonMailMCP` (#32).

## 0.4 — Drafts and attachments

- ✅ Tool: `create_draft`: a new plain-text draft in Drafts, nothing sent (#130). No `list_drafts`: `list_messages` on Drafts does it.
- ✅ Tool: `update_draft`: the new version in Drafts, the old one to Trash on Bridge 3.23.1 or later, otherwise left in Drafts with a note (#132).
- ✅ `trash_message` takes drafts, on Bridge 3.23.1 or later (#134).
- Tool: `create_reply_draft` (threading headers: `In-Reply-To`, `References`).
- ✅ `remove_label` (#55), which completes organizing: `UID EXPUNGE` in the label only, the owner's one exception to "no EXPUNGE".
- ✅ Tool: `view_image`: Claude sees the images in a message, attachments and inline images, never remote ones (#106).
- ✅ Tool: `save_attachment`: saves an attachment, only into the owner's attachment folder, never opened by the server (#107).

## 0.5 — macOS, Linux and packaging

- `setup`, keyring and Claude registration tried on macOS and Linux, and fixed where they differ (#66).
- The setup wizard on macOS and Linux (#91).
- Homebrew tap, Linux `.deb`/`.rpm`, a Claude Desktop extension (MCPB) (#33).
- More AI assistants than Claude (#128): setup and the wizard register the server in OpenAI Codex, GitHub Copilot in VS Code, Cursor, Gemini CLI and LM Studio; texts say "the assistant"; the website says where each one sends the mail.
- Ask Claude which version is installed and whether an update exists, and how to update (#125): `proton_mail_mcp_version` without network, `proton_mail_mcp_update` only when asked (the one connection outside localhost, a written guardrail exception first).

Before sending, so that organizing and drafts have been in daily use for a while when 0.6 starts.

## 0.6 — Send on command

- `internal/send`: SMTP over STARTTLS; the only package allowed to send.
- Tool: `send_draft` — sends an existing draft, refused unless `PROTON_ALLOW_SEND=1`, moves the draft away after sending.
- Guardrail tests: refused without the flag, refuses non-drafts, nothing else reaches SMTP.
- README: configuring each supported client to always ask before `send_draft`; a client that can't ask for every call is not supported for sending (#128).

## Installation

Part of a working product: a user sets it up without editing files by hand, on Windows, macOS and Linux. Design and research: [docs/installation-research.md](docs/installation-research.md).

- ✅ `proton-mail-mcp setup` (#30):
  - detect Bridge;
  - pin its certificate (no export);
  - ask for the Bridge password with hidden input, and keep it in the OS credential store;
  - register the server in Claude.
- ✅ `proton-mail-mcp check` and clear startup errors (#31).
- ✅ Registration in the config Claude Desktop (Microsoft Store) really reads (#42).
- ✅ Graphical setup and installer on Windows (0.3).
- Tried on Windows; macOS and Linux in 0.5 (#66).

## Later (ideas, not commitments)

- Optional direct Proton API (`go-proton-api`) instead of Bridge — unofficial API, its own risk assessment first.
