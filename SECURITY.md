# Security Policy

## Supported Versions

Only the latest release receives security fixes.

| Version | Supported |
|---|---|
| Latest release | ✅ |
| Older versions | ❌ |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Use [GitHub private vulnerability reporting](https://github.com/dvalfrid/proton-mail-mcp/security/advisories/new).

Include as much of the following as possible:

- Type of issue (e.g. send-guard bypass, credential exposure, prompt injection leading to an action)
- Steps to reproduce
- Affected version
- Potential impact

## Response Timeline

| | Target |
|---|---|
| Acknowledgement | Within 7 days |
| Patch release | Within 14 days of confirmed vulnerability |

## Threat model

proton-mail-mcp runs as a local process started by an MCP client (Claude). It holds the Proton Mail Bridge password and can read, organize and draft the owner's email. What it protects, and against what:

| Asset | Threat | Protection |
|---|---|---|
| Owner's outgoing mail | An AI assistant, possibly steered by a malicious email, sends mail the owner didn't approve | Only existing drafts can be sent (`send_draft`), only with `PROTON_ALLOW_SEND=1`, and the MCP client asks the owner before every send |
| Owner's mailbox | Mail permanently deleted | No expunge anywhere; "delete" moves to Trash. Enforced by lint (`forbidigo`) and tests |
| Bridge password | Leaked through logs, tool output, errors or the repo | Kept in the OS credential store (from `setup`), or in `.env`/the environment; typed by the owner in a terminal, never passed through the AI client; held as a type that never prints; never logged or returned; `.env` git-ignored and denied to Claude Code. **macOS caveat:** the credential-store library creates Keychain items through `/usr/bin/security`, so other programs running as the same user can read the item without a prompt, no worse than a file only the owner can read |
| Bridge password | A local program listens on Bridge's port (Bridge not running) and impersonates it to capture the login | TLS always verified against Bridge's pinned certificate (pinned by `setup`, which reads it without trusting it and, on Windows, checks that the port belongs to Proton's signed `bridge.exe`); the handshake fails before the password is sent |
| Mail content | Sent off the machine | Bridge only on `127.0.0.1`/`localhost`; non-local hosts refused at start-up. Content goes to the AI only when a tool returns it |
| The AI's instructions | Prompt injection inside an email | Mail content is returned marked as untrusted data; HTML reduced to text; no remote content fetched; attachments never opened |

In scope: anything that gets around one of the protections above. Out of scope: Proton Mail Bridge itself (report to Proton), and the MCP client's own approval UI.

Full rules: [STANDARDS.md — Security guardrails](STANDARDS.md#security-guardrails).
