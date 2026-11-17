# Troubleshooting

When proton-mail-mcp can't do something, the error Claude shows ends with a **Help** link to one of the sections below. Each says what the message means and how to fix it.

**First step, always:** check the connection.

- **On Windows:** open **Proton Mail MCP** from the Start menu. It opens on its **Status** page when something is wrong. Each step has a green or red mark; a red one says what to do and has a *Help with this* link to its section below. After fixing it, choose **Check again**.
- **In a terminal** (any system, or for a report): run

  ```
  proton-mail-mcp check
  ```

  If you installed by hand rather than on the PATH, use the full path, e.g. `C:\Tools\proton-mail-mcp\proton-mail-mcp.exe check`.

Both run the same check, so they always agree. It tests every link in the chain (settings, Bridge, certificate, login, SMTP, registration in Claude), says which one is broken and what to do, and never shows the password.

*Run setup again*, below, means: on Windows open Proton Mail MCP and choose **Set up again** (on its Status or About page); in a terminal, run `proton-mail-mcp setup`.

## Not set up

**Message:** "proton-mail-mcp isn't set up on this computer …"

The server found no settings: no username, Bridge password or pinned certificate. The tools stay available so Claude can tell you this, but they can't reach your mailbox.

**Fix:** on Windows, open **Proton Mail MCP** from the Start menu, which starts setup; elsewhere, run `proton-mail-mcp setup` in a terminal. It finds Bridge, pins its certificate, asks for the Bridge username and password (typed hidden, in the terminal only, never in a chat) and saves them. Then restart Claude Desktop, or start a new Claude Code session.

## Bridge not running

**Message:** "no answer from Proton Mail Bridge on 127.0.0.1:1143 …"

Nothing answers on Bridge's port. Bridge isn't started, isn't signed in, or uses another port.

**Fix:**
1. Start Proton Mail Bridge and make sure your account is signed in and synced.
2. If Bridge shows another IMAP port (Bridge → your account → Mailbox details), run `proton-mail-mcp setup -imap-port <port>` in a terminal.
3. Check again: **Check again** on the Status page, or `proton-mail-mcp check`.

## Login failed

**Message:** "login refused by Proton Mail Bridge …"

Bridge is running but didn't accept the username or the Bridge password. The Bridge password is not your Proton account password, and it changes when Bridge is reset or the account is removed and added again.

**Fix:** run setup again and paste the Bridge password from Bridge → your account → Mailbox details.

## Certificate does not match

**Message:** "… does not present the pinned Bridge certificate …"

The program on Bridge's port presents a different TLS certificate than the one pinned at setup. Usually Bridge was reinstalled or reset and made a new certificate. It could also be another program on that port, which is why the server refuses to send it the password.

**Fix:** check the connection (above). On Windows it shows which program owns the port and whether it is Proton's signed Bridge. If it is Bridge, run setup again to pin the new certificate.

## Not Proton Mail Bridge

**Check says:** "does not identify itself as Proton Mail Bridge", or a note on *the program on Bridge's port*.

Something answers on Bridge's port, but it doesn't greet like Proton Mail Bridge, or (on Windows) the program that owns the port isn't a validly signed `bridge.exe` under Program Files. The server won't send the Bridge password to a program it can't tell is Bridge.

**Fix:**
1. Look at the path the check shows. Proton Mail Bridge updates itself and can then run from its own updates folder in your profile (`%APPDATA%\protonmail\bridge-v3\updates\…\bridge.exe`); a path like that, signed, is Bridge. It is only a note, not a failure.
2. Any other program: find out what it is before going on. If Bridge uses another port, run `proton-mail-mcp setup -imap-port <port>` in a terminal, with the IMAP port Bridge shows.

## SMTP not reachable

**Check says:** "SMTP on 127.0.0.1:1025: …"

Bridge's IMAP port works but its SMTP port doesn't answer, or answers with another certificate. Reading, organising and drafts don't use SMTP, so they keep working.

**Fix:** if Bridge shows another SMTP port (Bridge → your account → Mailbox details), run `proton-mail-mcp setup -smtp-port <port>` in a terminal. If the certificate differs, find out what program is on that port.

## Timeout

**Message:** "… no answer from Bridge in time …" or "search in … took too long"

Bridge didn't answer within 30 seconds (55 for a search). Bridge has no search index: a search on sender, recipient, subject or text reads every message in the folder, which takes seconds in large folders such as All Mail, longer while Bridge is still syncing or has to fetch messages from Proton.

**Fix:** ask Claude to narrow the search by date (`since` / `before`) or to search a smaller folder. If plain listing also times out, check that Bridge isn't busy syncing, then run `proton-mail-mcp check`.

## Folder renumbered

**Message:** "… was renumbered by Bridge since the uids were listed; list it again"

Bridge renumbers every message in a folder after a resync (a repair, or a change of address mode). Message numbers Claude got earlier then point at other messages, so the server refuses to use them. Nothing was changed.

**Fix:** none needed: Claude lists the folder again and continues. If it keeps happening, Bridge is resyncing; wait until it is done.

## Change failed

**Message:** "the change may be partly done: list the folder again …", sometimes after "no connection from Proton Mail Bridge to Proton's servers …"

Marking, starring, moving, labelling and trashing are done by Bridge on Proton's servers before it answers. If Bridge can't reach Proton (no internet, Proton down, Bridge signed out) it refuses the change. With several messages in one call, some may already have changed when it stopped.

**Fix:** check the internet connection and that Bridge shows your account signed in and synced. Then let Claude list the folder again to see what changed, and retry only the rest. Nothing is ever deleted by these tools: a message that was moved is in its new folder or still in the old one.

## Change limit reached

**Message:** "this server run has reached its limit of changed messages …"

The tools that change messages (mark read or unread, star, move, label, trash) together change at most 200 messages each time the server runs. An email can contain instructions aimed at Claude; the limit stops an assistant fooled by one from reorganising the whole mailbox before you notice. Calls that were refused before anything was sent to Bridge don't count.

**Fix:** if you did ask for the changes, restart Claude Desktop (or start a new Claude Code session): that restarts the server and the count. For a bigger clean-up, set `PROTON_MAX_CHANGES` to a higher number in the server's environment, or in a `.env` next to the program, and restart. Check what was changed so far before you raise it.

## Search finds nothing

Bridge compares search terms with the raw, still-encoded message, so words with letters such as å, ä, ö or é usually find nothing, even when the mail exists.

**Fix:** search for an ASCII part of the word, or by sender or date, and let Claude read the candidates. Claude is told this, but you can ask for it.

## Internal error

**Message:** "internal error in proton-mail-mcp; the server is still running …"

A bug: the call hit an unexpected state. The server caught it, so other calls still work, and the call can be retried.

**Fix:** retry. If it repeats, please [report it](#reporting-a-problem) with the log lines around the error.

## Claude does not show the tools

The `proton-mail` tools are missing in Claude, or the server shows as disconnected.

**Fix:**
1. Check the connection (above); the last step, *Added to Claude*, shows which Claude configs have the server, and the About page lists the Claude apps. If one is missing, run setup again and let it register.
2. Quit Claude Desktop completely (also from the system tray) and start it again; it starts the server only at launch. In Claude Code, start a new session.
3. Look in the [log](#where-the-logs-are).

## Where the logs are

The server writes its log to stderr, and the Claude client saves it. The log has folder names, uids, counts and errors, never message bodies, subjects or the password.

| Client | Log |
|---|---|
| Claude Desktop, Windows | `%LOCALAPPDATA%\Claude\logs\mcp-server-proton-mail.log` (seen on Windows 11 with the Microsoft Store build) |
| Claude Desktop, macOS | `~/Library/Logs/Claude/mcp-server-proton-mail.log` (documented by Anthropic; not tried here) |
| Claude Code | run `claude --debug`, or `/mcp` in a session to see the server's status |

## Reporting a problem

Open an issue at <https://github.com/dvalfrid/proton-mail-mcp/issues> with:

- what you asked Claude to do, and the error it showed;
- the output of `proton-mail-mcp check` and `proton-mail-mcp -version` (the terminal output is easier to paste than the Status page);
- the log lines around the error.

The repository is public. Remove email addresses and anything from your mailbox before posting; describe a message rather than pasting it.
