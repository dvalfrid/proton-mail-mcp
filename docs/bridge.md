# Proton Mail Bridge as an IMAP/SMTP server

What this covers: how Proton Mail Bridge behaves toward a local IMAP/SMTP client, read from the source code wherever possible. Researched on 2026-11-11.

Code read (every source link below points at these commits):

| Repository | Commit | Notes |
|---|---|---|
| [ProtonMail/proton-bridge](https://github.com/ProtonMail/proton-bridge/tree/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2) | `36d7b6a` (2026-11-06) | `master`. The newest `Changelog.md` entry is 3.27.2. |
| [ProtonMail/gluon](https://github.com/ProtonMail/gluon/tree/7e800978ab4a9942829eb6116e33ea0278c03e91) | `7e80097` | The exact gluon commit Bridge pins in `go.mod` (`v0.17.1-0.20260901104218-7e800978ab4a`). |
| [ProtonMail/go-proton-api](https://github.com/ProtonMail/go-proton-api/tree/8a62023d3e38cb29749bcfc01bf44eafda6cec82) | `8a62023` | The commit Bridge pins. |
| [ProtonMail/go-smtp](https://github.com/ProtonMail/go-smtp/tree/2b3d50599865839dfccb6bf4805996fc3d476f29) | `2b3d505` | Proton's fork, wired in through a `replace` in Bridge's `go.mod`. |

Labels used below:

- **Verified in source:** read in the code at the commits above. The file, the function and a link are given.
- **Documented:** stated on a Proton page or in a changelog. A link is given.
- **Inferred / unconfirmed:** a conclusion not shown directly. It says what would confirm it.

In the text, "Proton" means the Proton servers and API, "gluon" means the IMAP server library inside Bridge, and "the connector" means Bridge's glue code between gluon and the Proton API (`internal/services/imapservice/connector.go`).

---

## 1. Capabilities

- **Verified in source.** gluon defines eight capabilities: `IMAP4rev1`, `STARTTLS`, `IDLE`, `UNSELECT`, `UIDPLUS`, `MOVE`, `ID` and `AUTH=PLAIN` ([imap/capabilities.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/imap/capabilities.go)).
  - Before login only `IMAP4rev1`, `STARTTLS`, `IDLE`, `ID` and `AUTH=PLAIN` are advertised (`IsCapabilityAvailableBeforeAuth`).
  - Each session starts with `IMAP4rev1 UNSELECT UIDPLUS MOVE ID` ([internal/session/session.go `New`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/session/session.go#L120)).
  - `IDLE` is added unless a remote feature flag, `InboxBridgeImapIdleCapabilityDisabled`, turns it off. So Proton can switch IDLE off remotely.
  - `AUTH=PLAIN` is added unless the "disable IMAP AUTHENTICATE" setting is on.
  - `STARTTLS` is added when a TLS config is set (`SetTLSConfig`).
- **Verified in source.** None of these are advertised or implemented: CONDSTORE, QRESYNC, ESEARCH, SPECIAL-USE (the capability), LITERAL+, COMPRESS, NAMESPACE, ENABLE, QUOTA, SORT or THREAD.
  - `imap/command/` has a parser only for the base commands (APPEND, AUTHENTICATE, CAPABILITY, CHECK, CLOSE, COPY, CREATE, DELETE, EXAMINE, EXPUNGE, FETCH, ID, IDLE, LIST, LOGIN, LOGOUT, LSUB, MOVE, NOOP, RENAME, SEARCH, SELECT, STARTTLS, STATUS, STORE, SUBSCRIBE, UID, UNSELECT, UNSUBSCRIBE).
  - SORT/THREAD (RFC 5256) is an open feature request: [gluon#378](https://github.com/ProtonMail/gluon/issues/378).
- **Verified in source.** gluon still sends special-use *attributes* (`\Trash`, `\Sent`, …) in LIST, although it doesn't advertise the SPECIAL-USE capability (see section 4).

## 2. SEARCH

**Supported keys. Verified in source.** These are the parser ([imap/command/search.go `handleSearchKey`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/imap/command/search.go)) and the evaluator ([internal/state/mailbox_search.go `buildSearchOp`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go)):

- All the RFC 3501 keys: ALL, ANSWERED, BCC, BEFORE, BODY, CC, DELETED, DRAFT, FLAGGED, FROM, HEADER, KEYWORD, LARGER, NEW, NOT, OLD, ON, OR, RECENT, SEEN, SENTBEFORE, SENTON, SENTSINCE, SINCE, SMALLER, SUBJECT, TEXT, TO, UID, UNANSWERED, UNDELETED, UNDRAFT, UNFLAGGED, UNKEYWORD and UNSEEN.
- Sequence sets.
- Parenthesised lists.
- Keys are combined with AND.

**How text matching works. Verified in source.**

- **FROM, TO, CC, BCC, SUBJECT and HEADER** are a case-insensitive substring match against the header value.
  - Both sides go through `strings.ToLower` and then `strings.Contains`.
  - The value comes from `rfc822.Header.Get` ([rfc822/header.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/rfc822/header.go#L114)). That unfolds continuation lines but does **not** decode RFC 2047 encoded-words.
- **BODY** is a case-insensitive substring match against the **raw** bytes after the top-level header: `rfc822.Parse(literal).Body()`, with `bytes.ToLower` and `bytes.Contains` ([`buildSearchOpBody`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go#L368)).
  - That includes MIME boundaries and part headers.
  - It also includes **still-encoded** content: quoted-printable and base64.
  - gluon has a `DecodedBody()` helper, but SEARCH doesn't use it.
- **TEXT** is the same, but over the whole raw message, header included ([`buildSearchOpText`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go#L672)).
- **Lower-casing is Unicode-aware.** Go's `strings.ToLower` and `bytes.ToLower` handle UTF-8, so `Å` matches `å` *when both are plain UTF-8 bytes*.

**CHARSET. Verified in source.** `SEARCH CHARSET <name>` is parsed. The name is looked up with `ianaindex.IANA.Encoding`; an unknown name gives `NO [BADCHARSET]` ([internal/session/handle_search.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/session/handle_search.go#L15)). The search terms are decoded with that charset. Without a CHARSET the term bytes are used as they are.

**Non-ASCII terms such as "å".** Matching works on bytes as stored, so it depends on how Bridge wrote the message. Bridge rebuilds every message itself ([pkg/message/build.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/pkg/message/build.go)):

- **Verified in source.** Bridge writes `Subject` with `mime.QEncoding.Encode("utf-8", …)` (`setUTF8EncodedHeaderIfNeeded`, L486).
  - So a non-ASCII subject is stored as an RFC 2047 encoded-word, for example `=?utf-8?q?...?=`.
  - `SEARCH SUBJECT "å"` therefore will **not** match it.
  - Addresses go through `mail.Address.String()`, which also uses RFC 2047 for non-ASCII display names.
- **Verified in source.** Every `text/*` part is written as `Content-Transfer-Encoding: quoted-printable` (`getTextPartHeader`, L503). Attachments are written as base64 (`getAttachmentPartHeader`, L518).
  - So `SEARCH BODY "å"` doesn't match a body that contains å: the stored bytes are `=C3=A5`.
  - An ASCII word can also be missed if quoted-printable splits it with a soft line break (`=\r\n`).
- **Inferred / unconfirmed.** Put together: SUBJECT/FROM/BODY searches are reliable only for ASCII terms that appear unencoded. Non-ASCII terms and words in HTML-only or base64 parts will often be missed. A small test against a real Bridge would confirm this: a message with "Smörgås" in the subject and body, then SEARCH for it.

**Dates. Verified in source.**

- **INTERNALDATE keys:**
  - `SINCE` truncates the message's INTERNALDATE to a date. It does that in the time's own zone, then compares with the search date at UTC midnight.
  - `BEFORE` compares the full INTERNALDATE timestamp with the search date at UTC midnight ([L360](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go#L360), [L633](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go#L633)).
  - `ON` uses `Truncate(24h)` on both sides, which works in UTC.
  - Search dates are parsed as midnight UTC ([imap/command/date_time.go `ParseDateText`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/imap/command/date_time.go#L195)).
  - In practice day boundaries are UTC, not local time.
- **`Date:` header keys** (`SENTBEFORE`, `SENTON` and `SENTSINCE`) parse each message's `Date:` header.
  - If that header can't be parsed, the op returns an error, and that error fails the whole SEARCH command ([L614](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_search.go#L614)).
  - Bridge does sanitise `Date` when it builds messages (`SanitizeDate: true`), which makes such a failure less likely.

**Performance. Verified in source.** SEARCH visits every message in the mailbox, in parallel (`runtime.NumCPU() / activeSearches` goroutines). The cost depends on the keys:

- **Flag, UID and sequence keys** need no I/O.
- **Date and size keys** read one database row per message.
- **Header keys, BODY and TEXT** load the *full literal* of every message from gluon's on-disk cache.
  - If a message isn't cached, gluon downloads it from Proton through the connector ([internal/state/state.go `getLiteral`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/state.go#L705)).
  - So a BODY search on a large folder costs work proportional to the folder's total size.
- There is no search index.
- **Documented.** Bridge 3.0.6 release notes: "Full support of IMAP SEARCH" ([early releases](https://proton.me/download/bridge/early_releases.html)).

## 3. UIDs and dates

**UID assignment. Verified in source.**

- **UIDs come from SQLite.** Each mailbox has its own table whose UID column is `INTEGER PRIMARY KEY AUTOINCREMENT` ([internal/db_impl/sqlite3/v1/mailbox.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/db_impl/sqlite3/v1/mailbox.go#L42)).
  - Messages are inserted in the order of the list passed in ([write_ops.go `AddMessagesToMailbox`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/db_impl/sqlite3/write_ops.go#L222)).
  - So the order in which Bridge hands gluon the messages decides UID order.
- **The initial sync works newest-ID first.**
  - **Metadata stage.** Bridge pages message metadata with `MessageFilter{Desc: true}` and `EndID` pagination ([stage_metadata.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/syncservice/stage_metadata.go#L170)).
    - go-proton-api sends that request with `Sort: "ID"` ([message.go `GetMessageMetadataPage`](https://github.com/ProtonMail/go-proton-api/blob/8a62023d3e38cb29749bcfc01bf44eafda6cec82/message.go#L294)).
    - So messages are listed in **descending Proton message ID**, not by date.
  - **Download stage.** Batches are downloaded with `parallel.MapContext`, which keeps the input order ([stage_download.go `autoDownloadRate`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/syncservice/stage_download.go)).
  - **Build and apply stages.**
    - Each batch is built.
    - It is then sent as **one** `imap.MessagesCreated` update, in the same order ([sync_update_applier.go `ApplySyncUpdates`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/sync_update_applier.go#L59)).
    - gluon's `applyMessagesCreated` appends to each mailbox's list in update order ([connector_updates.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/backend/connector_updates.go#L273)).
  - **Result:** within every mailbox, messages from the initial sync get UIDs in **descending Proton-ID order**. The message with the highest ID gets the lowest UID.
- **New mail after the sync** comes from the event stream and is appended. It gets UIDs higher than every synced message.
- **Inferred / unconfirmed.** Proton message IDs are opaque strings. Nothing in the code says they follow time order, but it's likely they roughly follow creation order. If so, a freshly synced folder has its UIDs roughly *reverse*-chronological, and later mail continues forward. So **UID order isn't date order** in either direction. To confirm: compare UID order with INTERNALDATE order on a freshly synced account.
- **A changed message gets a new UID.** When Proton sends an update that changes a message's content, gluon removes the old message and inserts a new internal message ([connector_updates.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/backend/connector_updates.go#L740)). Clients see an EXPUNGE followed by a new UID.
  - This happens for drafts and sent messages that change.
  - It doesn't happen for flag or label changes.
  - **Documented.** Changelog 3.0.4: "Update draft event means delete old and create new message".

**Stability across restarts and resyncs.**

- **Across a restart: stable. Verified in source.** UIDs and UIDVALIDITY live in gluon's SQLite database and survive a restart.
- **Across a resync: they change. Verified in source.**
  - These paths remove the gluon user *with its data* and sync again from scratch:
    - a "repair" or refresh event (`HandleRefreshEvent`, [service.go L291](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/service.go#L291));
    - switching combined/split address mode (`setAddressMode`, L702);
    - a lost or corrupted gluon database.
  - They call `removeConnectorsFromServer(..., true)` and `ClearSyncStatus`.
  - The recreated mailboxes get a **new UIDVALIDITY**, so every UID is invalidated.
  - UIDVALIDITY is a timestamp: seconds since 2023-02-01 ([imap/uid_validity_generator.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/imap/uid_validity_generator.go#L24)). So a new value is always higher than the old one.
  - **Documented.** Changelog 3.0.0: "Bump UID validity when clearing sync status". Release notes 3.1.0: UIDVALIDITY is set "from the current time".

**INTERNALDATE. Verified in source.** INTERNALDATE is `time.Unix(message.Time, 0)` from Proton's message metadata, or `time.Now()` when `Time` is 0 ([helpers.go `toIMAPMessage`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/helpers.go#L123)).

- **APPEND.** The date-time argument is **ignored**: the connector's `CreateMessage` takes `_ time.Time`. The stored INTERNALDATE is Proton's `Time` for the imported message.
- **Inferred / unconfirmed.** What Proton sets `Time` to on import is not shown in this code: it could be the `Date:` header or the import time. Checking the INTERNALDATE after an APPEND with an old `Date:` header would show which.
- **Verified in source.** Bridge also adds `X-Pm-Date` (the same `Time`, in UTC) to every message it builds (`defaultMessageJobOpts`, [sync_build.go L48](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/sync_build.go#L48)).

## 4. Folders and labels

**Mailbox names. Verified in source.** These come from [imap_updates.go `GetMailboxName`, `newSystemMailboxCreatedUpdate`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/imap_updates.go#L115) and [helpers.go `WantLabel`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/helpers.go#L141).

- **System labels** appear under their own names: `INBOX`, `Drafts`, `Sent`, `Starred`, `Archive`, `Spam`, `Trash`, `All Mail` and `Scheduled`. The API calls the last one "All Scheduled".
  - All of them have `\Noinferiors`.
  - Special-use attributes:

    | Mailbox | Attribute |
    |---|---|
    | Trash | `\Trash` |
    | Spam | `\Junk` |
    | All Mail | `\All` |
    | Archive | `\Archive` |
    | Sent | `\Sent` |
    | Drafts | `\Drafts` |
    | Starred | `\Flagged` |

  - Other system labels (All Drafts, All Sent, …) are hidden.
- **User folders** appear as `Folders/<path>`. **User labels** appear as `Labels/<path>`.
- **Contact groups** are a label type, but they're filtered out and never become mailboxes.
- **All Mail can be hidden.** Bridge has a "Show All Mail" setting; see `ShowAllMail` and `GetMailboxVisibility`.
- **Documented.** "Labels in Bridge" ([proton.me/support/labels-in-bridge](https://proton.me/support/labels-in-bridge)) says labels show as subfolders of a Labels folder.

**COPY. Verified in source.** COPY to any mailbox calls `LabelMessages(target)` (`AddMessagesToMailbox`, [connector.go L387](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L387)).

- **COPY to `Labels/X` adds the label.**
- **COPY to All Mail or Scheduled** is refused (`ErrOperationNotAllowed`).
- **COPY to a folder.**
  - **Inferred / unconfirmed.** Proton folders are exclusive, so labelling a message with a folder ID moves it on Proton's side.
  - The source mailbox then loses the message through a later event, not straight away.
  - **Documented** in part: Proton's page says copying "still files the message" while keeping its labels.

**MOVE. Verified in source.** MOVE calls `LabelMessages(target)` and then decides whether to drop the message from the source mailbox ([connector.go `moveMessagesWithUnlabelCallOnFolders` L661, `moveMessagesWithoutUnlabelCallOnFolders` L625](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L625); gluon [actions.go `actionMoveMessages`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/actions.go#L542)).

- **It drops the message from the source** (`shouldExpungeOldLocation = true`) when the source is a label, or the target is a folder or system mailbox.
  - In those cases Bridge also unlabels the source.
  - Which variant runs depends on the remote flag `InboxBridgeUnlabelCallOnFoldersDisabled`. With the flag set, the source is unlabelled only when it is a label.
- **MOVE from a folder (for example INBOX) to `Labels/X` doesn't remove it from the source.** The label is added and the message stays in INBOX. It behaves like COPY.
- **MOVE from `Labels/X` to INBOX** removes the label and files the message in INBOX. This matches the documented "moving out of a label folder removes the label".
- **Refused moves:** INBOX↔Sent, and anything to or from All Mail or Scheduled.
- **Changelog** (3.24.0 BRIDGE-451, then 3.24.1 BRIDGE-522): the unlabel-on-folders behaviour was removed and then restored behind a feature flag. So it has changed recently and is controlled remotely.

**Removing a message from `Labels/X`. Verified in source.** EXPUNGE or MOVE out of `Labels/X` calls `UnlabelMessages(X)`. The message itself stays in its folder.

**Starred and `\Flagged`. Verified in source.** They are the same thing, in both directions.

- Proton's starred state becomes `\Flagged` (`BuildFlagSetFromMessageMetadata`, [helpers.go L55](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/helpers.go#L55)).
- STORE `+FLAGS \Flagged` labels the message Starred; `-FLAGS` unlabels it (`MarkMessagesFlagged`, [connector.go L707](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L707)).
- COPY into `Starred` adds the Starred label.
- APPEND with `\Flagged` adds Starred.

## 5. Flags

**Verified in source.** Flags are handled by gluon's `applyMessageFlagsAdded` / `…Removed` ([internal/state/updates.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/updates.go#L97)) and by the connector.

| Flag | Synced to Proton? | How |
|---|---|---|
| `\Seen` | Yes | `MarkMessagesRead` / `MarkMessagesUnread` |
| `\Flagged` | Yes | Starred label |
| `$Forwarded` / `Forwarded` | Yes | `MarkMessagesForwarded`. Either spelling sets both. |
| `\Deleted` | **No** | A per-mailbox, local flag in gluon's DB (`SetMailboxMessagesDeletedFlag`). It only matters for EXPUNGE. |
| `\Answered` | **No** on STORE | Shown from Proton's replied flags. STORE only saves it locally, though APPEND with `\Answered` sets Proton's "replied" flag on import. |
| `\Draft` | **No** | Shown from Proton's draft state. A STORE is saved locally only. |
| Custom keywords | **No** | STORE accepts any keyword (only `\Recent` is refused) and saves it in gluon's local DB. They don't reach Proton and are lost on a resync. |

- **PERMANENTFLAGS.** For ordinary mailboxes PERMANENTFLAGS lists `\Seen \Flagged \Deleted $Forwarded Forwarded` (`defaultMailboxFlags`). For All Mail it lists only `\Seen \Flagged`.
- **`\Recent`** is read-only.
- **FETCH marks messages read.** FETCH of `BODY[]` (without PEEK), `RFC822` or `RFC822.TEXT` sets `\Seen`, but not when the mailbox was opened with EXAMINE ([mailbox_fetch.go L155](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_fetch.go#L155)).

## 6. Deletion

**`\Deleted` + EXPUNGE. Verified in source.**

- **What runs.** EXPUNGE (and UID EXPUNGE) removes the `\Deleted` messages from the selected mailbox (`Mailbox.Expunge` → `actionRemoveMessagesFromMailbox`). That calls the connector's `RemoveMessagesFromMailbox` ([connector.go L413 / L521](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L413)).
- **In a normal folder (INBOX, `Folders/…`, Archive, Spam).** Nothing is deleted on Proton.
  - The default variant calls `UnlabelMessages(folder)`.
  - With the remote flag set, nothing is sent to Proton for folders. The message just disappears from the local mailbox.
  - **Inferred / unconfirmed.** In the unlabel variant the message probably ends up only in All Mail.
  - **Documented.** Changelog 3.0.4: "Disable perma-delete for expunge on Spam folder".
- **In `Labels/X`.** EXPUNGE removes the label.
- **In Trash or Drafts.** EXPUNGE is a **permanent delete**.
  - For each message Bridge reads its labels from Proton.
  - It ignores the mailbox being expunged and All Mail, All Drafts, All Sent and All Scheduled.
  - If no wanted label remains, it calls `DeleteMessage`, the API's delete route `PUT /mail/v4/messages/delete` (go-proton-api `DeleteMessage`).
  - A message that still has another label (for example a user label or Starred) is not deleted.
  - Drafts is included because, in Bridge's own words, some clients replace a draft by deleting it and appending a new one.

**Moving to Trash. Verified in source.**

- MOVE to Trash calls `LabelMessages(Trash)`. Trash is a system mailbox, so the source is also unlabelled (`moveMessages…`). Nothing is deleted.
- **Documented.** Release notes 3.5.0: "When a message is added to the trash, Bridge removes all labels" ([early releases](https://proton.me/download/bridge/early_releases.html)).
- **Documented.** Changelog 3.23.1 (BRIDGE-488): "Drafts moved to trash should not be perma-deleted".
- **What happens to it in Trash.** It stays there, recoverable, until the user empties Trash or the "Auto-delete unwanted messages" setting removes it after 30 days.
  - **Documented** in [proton.me/support/auto-delete-unwanted-messages](https://proton.me/support/auto-delete-unwanted-messages). The setting is off by default and needs a paid plan.
  - **Inferred / unconfirmed.** Whether the API strips user labels from the message itself, or whether only Bridge does: the source shows Bridge only unlabelling the source mailbox. Checking in the web app after a MOVE to Trash would settle it.
  - **Verified (observed, issue #54).** On a real Bridge (2026-11-12), a message that had a user label, moved from a folder to Trash with UID MOVE, was still listed under that `Labels/X` over IMAP right after the move. So Bridge did not strip that label at that point, at least not locally. Whether the web app shows the label is still unchecked.

## 7. Drafts

**APPEND to Drafts creates a new Proton draft. Verified in source.**

- **Validation.** gluon skips its header validation for Drafts ([handle_append.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/session/handle_append.go#L15)). It always creates a new message there, even when the literal has an `X-Pm-Gluon-Id` header ([state/mailbox.go L180](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox.go#L180)).
- **What the connector sends.** `CreateMessage` → `importMessage` → `createDraftWithParser` ([connector.go L974](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L974)) parses the MIME and calls the API's `CreateDraft` with:
  - Subject;
  - the body (the HTML part if there is one, otherwise plain text);
  - To/Cc/Bcc;
  - ExternalID (from `Message-ID`);
  - the sender set to the chosen account address.
- **Attachments** are then uploaded one by one (`UploadAttachment`).
- **What gets stored.** Bridge fetches the new draft back, rebuilds it as RFC 822, and stores *that* in gluon, not the bytes that were appended.
  - **The client gets the new UID** in `APPENDUID` (UIDPLUS).
  - **Other headers are lost.** `In-Reply-To`, `References` and custom headers from the appended literal are not passed to `CreateDraft`; it doesn't take a parent ID or action.
  - **Inferred / unconfirmed.** So Proton doesn't link a draft created by APPEND to the message it replies to. Threading for a reply is only set up when the message is sent; see section 8.
- **Sender address** (`getImportAddress`, [utils.go L52](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/utils.go#L52)):
  - In combined mode the `From` address is used if it is one of the account's own and allowed to send.
  - Otherwise the mailbox's default address is used.
- **Drafts are never marked unread.**

**No in-place update. Verified in source.** IMAP has no way to change a message.

- **From IMAP, "updating" a draft** means APPENDing a new draft and removing the old one.
  - **Removing the old one** by EXPUNGE in Drafts is a **permanent delete** (section 6).
  - MOVE to Trash keeps it, but in Trash.
- **When a draft is edited elsewhere** (for example in the web app), Bridge gets an update event. gluon replaces the message: EXPUNGE of the old UID, then a new UID. See section 3 and `onMessageUpdateDraftOrSent` in [service_message_events.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/service_message_events.go).
- **A draft's own ID is in its headers.** Every draft that Bridge builds carries `X-Pm-Internal-Id: <ProtonID>` and, in `References`, `<ProtonID@protonmail.internalid>` (`AddMessageIDReference`, [build.go L451](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/pkg/message/build.go#L451)). Section 8 explains why this matters.

## 8. SMTP sending via Bridge

**How a send works. Verified in source.** See `smtpSendMail` and `sendWithKey` in [internal/services/smtp/smtp.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/smtp.go#L55).

1. Parse the DATA.
2. Create a **new** draft through the API (`createDraft`).
3. Upload the attachments.
4. Work out each recipient's sending preferences (keys, contacts, mail settings).
5. Call `SendDraft`.

SMTP never sends an existing draft directly.

**Sending an existing draft, indirectly. Verified in source.**

- `getParentID` ([L293](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/smtp.go#L293)) looks at `In-Reply-To` and `References`.
- For each `<ID@protonmail.internalid>` reference it reads the message.
  - **A sent or received message** becomes the parent: threading, and reply or forward state.
  - **A draft** is recorded as "draft to delete".
- **Only after a successful send** does Bridge call `DeleteMessage` on those drafts ([L284](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/smtp.go#L284)). That is a permanent delete.
  - **Documented.** Changelog 3.7.1: "Only delete drafts after message has been Sent".
- **So:**
  1. FETCH a draft from Drafts. Its `References` already includes its own internal ID.
  2. Submit it over SMTP.
  3. Bridge sends a *copy* built from it and then deletes the original draft.
- If the draft is a reply, its `References` also carry the parent's ID, so threading and the "replied" state come back at send time. That parent's ID only appears if it was in the draft's own `References`, and section 7 notes that an APPEND loses them.
  - **Inferred / unconfirmed.** That this whole route works end to end. A test against a real Bridge would confirm it.

**Sent copy. Verified in source** on the Bridge side; the server side is inferred.

- Bridge doesn't APPEND to Sent. The message is sent by `SendDraft`, after which Proton holds it as a sent message, and it reaches the IMAP `Sent` mailbox through the event stream.
  - **Inferred / unconfirmed.** That Proton itself files the sent message under Sent. This follows from Proton's mail model rather than from any code read here.
- **Duplicate guard.** If a client APPENDs the same message to Sent within 90 s, the connector recognises it by hash and returns the already-sent message instead of importing a second copy (`CreateMessage` → `sendRecorder.HasEntryWait`, [connector.go L303](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/connector.go#L303)).
- **Silent skip of repeats.** If the *same* message (same hash and recipients) is submitted over SMTP again within 90 s, Bridge logs "duplicate" and returns **success without sending** (`TryInsertWait`, L84).

**Auth. Verified in source.**

- SMTP AUTH supports PLAIN (from go-smtp) and LOGIN (added by Bridge), and `AllowInsecureAuth = true` ([imapsmtpserver/smtp.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapsmtpserver/smtp.go#L50)).
- The username must be one of the account's **enabled** addresses. The password is the Bridge password ([useridentity/state.go `CheckAuth`](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/useridentity/state.go#L230)), compared in constant time.

**From addresses. Verified in source.**

- `MAIL FROM` must be one of the account's addresses (`GetAddr`; otherwise `ErrInvalidReturnPath`).
- If the message has a `From:` header, that address is used instead, and it must also be the account's, enabled and allowed to send.
- `createDraft` checks again that the sender is owned by the user (`ErrSenderAddressNotOwned`). It keeps a `+alias` part.
- So only the account's own addresses can send.

**Recipients. Verified in source.**

- The draft's `To` list is filtered down to addresses that were also given as `RCPT TO`.
- Any `RCPT TO` that is not in To/Cc/Bcc is added as Bcc.
- So every intended To/Cc recipient must also be given in `RCPT TO`.
- An empty recipient list is refused (`ErrInvalidRecipient`).

**Body type. Verified in source.** The parsed message must be `text/plain` or `text/html`. Other types give `ErrUnsupportedOutgoingMIME`.

**Size limits.**

- **SMTP.** Bridge doesn't set `MaxMessageBytes`, and in this go-smtp fork 0 means no limit and no `SIZE` advertised (**verified in source**, [go-smtp conn.go](https://github.com/ProtonMail/go-smtp/blob/2b3d50599865839dfccb6bf4805996fc3d476f29/conn.go#L270)).
  - The limit comes from the API: an "message too large" error from `CreateDraft` is mapped to `ErrMessageTooLarge`.
  - **Not found:** the exact API size limit for sending.
- **IMAP import.** For APPEND to folders other than Drafts, go-proton-api enforces "maximum import size of 50MB" ([message_import.go L30](https://github.com/ProtonMail/go-proton-api/blob/8a62023d3e38cb29749bcfc01bf44eafda6cec82/message_import.go#L30)).

**Throttling. Verified in source.** After 3 failed sends, each within 1 s of the previous, the account is refused for 20 s (`ErrTooManyErrors`; [smtp/accounts.go L32](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/accounts.go#L32)).

## 9. FETCH details

**What FETCH supports. Verified in source** ([internal/state/mailbox_fetch.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_fetch.go#L25)).

- **Attributes:** FLAGS, INTERNALDATE, RFC822.SIZE, ENVELOPE, BODY, BODYSTRUCTURE, UID, RFC822, RFC822.HEADER and RFC822.TEXT.
- **Macros:** ALL, FAST and FULL.
- **Body sections:** `BODY[...]` / `BODY.PEEK[...]` with section paths, `HEADER`, `HEADER.FIELDS (NOT)`, `TEXT` and `MIME`.
- **Partial ranges** `<offset.count>` are supported.

**What each part costs.**

- **ENVELOPE, BODY and BODYSTRUCTURE** come from gluon's database, built when the message was stored. They don't load the literal.
- **FLAGS, INTERNALDATE, RFC822.SIZE and UID** are also database reads, so they're cheap.
- **Any body section, partial or not**, loads the **whole** literal from the local cache (or downloads it), then cuts out the requested range.
  - A partial fetch saves transfer to the client, not work in Bridge.

**RFC822.SIZE. Verified in source.**

- It is the size of the literal gluon stores. That is Bridge's rebuilt message **plus** a header gluon inserts: `X-Pm-Gluon-Id: <internal id>` ([internal/ids/header.go](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/ids/header.go)).
- So the size matches what `BODY[]` returns, but not the size Proton reports or the size the sender sent.
- Messages also carry Bridge-added headers: `X-Pm-Internal-Id`, `X-Pm-External-Id`, `X-Pm-Date`, a `References` entry `<ID@protonmail.internalid>`, and `X-Original-Date` when the date was sanitised.

**Response order. Verified in source.** With more than 4 messages, or more than 1 when literals are needed, FETCH runs in parallel. Its untagged responses can come **out of sequence order** (`parallel.DoContext`, [L118](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox_fetch.go#L118)). Clients must match responses by sequence number or UID.

**Messages Bridge cannot decrypt or build. Verified in source.** They are still created, as a placeholder message that explains the error (`newMessageCreatedFailedUpdate` / `newFailedMessageLiteral`, [sync_build.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/sync_build.go)).

**Message size limits on FETCH.** Not found, beyond the 50 MB import limit above.

## 10. Other things an automating client should know

- **Initial sync.** These points are verified in source.
  - Mailboxes are created first. Messages then arrive in batches as `MessagesCreated` updates.
  - **While the sync runs, counts and SEARCH results are partial and keep growing.** Every batch adds messages with higher UIDs.
  - UIDs don't get renumbered when the sync finishes.
  - A sync that is interrupted resumes from `LastSyncedMessageID`.
  - **Inferred / unconfirmed.** Mail that arrives during the sync is handled by events and may get UIDs between sync batches. Either way, "highest UID = newest" doesn't hold.
- **Failed APPENDs.** A failed APPEND, for example an unparseable message or an API error, is stored in a local-only `Recovered Messages` mailbox, and the client still gets an error. A message too large to store is not ([state/mailbox.go `Append`](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox.go#L241)).
  - **Documented:** [What is the Recovered Messages folder](https://proton.me/support/what-is-the-recovered-messages-folder-in-bridge).
- **"Move like Outlook".** APPEND outside Drafts of a literal that carries a known, live `X-Pm-Gluon-Id` is treated as a copy of the existing message, not a new import (verified in source, [state/mailbox.go L180](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/state/mailbox.go#L180)). A client that builds a *new* message from a fetched one should strip `X-Pm-*` headers.
- **IDLE.** These points are verified in source.
  - IDLE is supported. Updates are batched for 500 ms (`idleBulkTime`, [builder.go L59](https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/builder.go#L59)).
  - IDLE can be switched off remotely by a feature flag (section 1).
  - **Documented.** Changelog: "Kill switch support for IMAP IDLE".
- **Connection limits. Verified in source.**
  - Bridge sets per-client connection limits: Apple Mail 60, fallback 25. For other clients the limit is 0, which presumably means no limit.
  - It also keeps a rolling connection counter with a threshold of 400 over 6×10 s buckets ([imapsmtpserver/imap.go L46](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapsmtpserver/imap.go#L46)).
  - **Inferred / unconfirmed.** What happens when that counter is exceeded was not traced.
- **Requests to Proton. Verified in source.** STORE of `\Seen` or `\Flagged`, COPY, MOVE and EXPUNGE each make Proton API calls before they reply. If the API can't be reached, the command fails (`handleStore` reports "failed request (API unreachable)").
- **Gaps in the IMAP command set.** Bridge has no SORT, THREAD, ESEARCH, CONDSTORE or QRESYNC (section 1). Sorting and change tracking must be done by the client.

## 11. Contacts

**Bridge doesn't expose contacts to local clients.**

- **Verified in source.** There is no contacts server in Bridge at this commit:
  - `carddav`, `caldav` and `ldap` appear in no Go file of proton-bridge;
  - the only non-test file that mentions vCard is `internal/services/smtp/smtp_prefs.go`, the code that works out sending preferences;
  - the only listeners are IMAP and SMTP (`internal/services/imapsmtpserver/`).
- **Verified in source.** Contact groups are a Proton label type, but `WantLabel` drops them, so there is no contacts mailbox either ([helpers.go L141](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapservice/helpers.go#L141)).
  - **Documented.** Changelog: "GODT-2805: Ignore Contact Group Labels".
- **Documented / not found.**
  - Proton's support pages describe Contacts as end-to-end encrypted and synced between Proton's *own* apps. They don't mention CardDAV ([Proton Contacts](https://proton.me/support/proton-contacts)).
  - A user request for CardDAV/CalDAV is open on Proton's feedback site ([UserVoice: DAV support](https://protonmail.uservoice.com/forums/932842-proton-calendar/suggestions/45730093-dav-support-for-contacts-calendar)).
  - **Not found:** any statement from Proton of plans for CardDAV, or for contacts in Bridge.
- GitHub issues are disabled on proton-bridge, so there is no issue tracker to check there.

**What Bridge does with contacts internally. Verified in source.**

- **When.** Only when sending.
- **What it reads.** `getRecipients` → `getContactSettings` ([smtp.go L534, L577](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/smtp.go#L577)) runs for each recipient:
  1. It calls `GetAllContactEmails(email)`.
  2. If there is a contact, it calls `GetContact(id)`.
  3. Then it calls `contact.GetSettings(userKR, email, CardTypeSigned)`.
  4. That reads the *signed* vCard and checks its signature with the user keyring. It returns: the MIME type, the encryption scheme (PGP/MIME or inline), sign, encrypt, encrypt-untrusted, and pinned public keys ([go-proton-api contact_types.go](https://github.com/ProtonMail/go-proton-api/blob/8a62023d3e38cb29749bcfc01bf44eafda6cec82/contact_types.go)).
- **How it's used.** Together with `GetPublicKeys` and the mail settings, this decides how each recipient's copy is encrypted and signed (`buildSendPrefs`, [smtp_prefs.go](https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/smtp/smtp_prefs.go)).
- **What it doesn't do.** Bridge never writes contacts and never shows them to clients.

**Other ways to reach contacts.**

1. **The Proton API through go-proton-api. Verified in source.** See [contact.go](https://github.com/ProtonMail/go-proton-api/blob/8a62023d3e38cb29749bcfc01bf44eafda6cec82/contact.go).
   - **Contact endpoints:**

     | Function | Request |
     |---|---|
     | `GetContact` | `GET /contacts/v4/{id}` |
     | `GetContacts`, `GetAllContacts`, `CountContacts` | `GET /contacts/v4` |
     | `GetContactEmails`, `GetAllContactEmails`, `CountContactEmails` | `GET /contacts/v4/emails?Email=` |
     | `CreateContacts` | `POST /contacts/v4` |
     | `UpdateContact` | `PUT /contacts/v4/{id}` |
     | `DeleteContacts` | `PUT /contacts/v4/delete` |

   - **The data.** A contact is metadata plus a list of vCards ([contact_card.go](https://github.com/ProtonMail/go-proton-api/blob/8a62023d3e38cb29749bcfc01bf44eafda6cec82/contact_card.go#L61)).
     - The metadata holds the ID, Name and ContactEmails (Name, Email). The API returns it unencrypted.
     - Each card has a type: `Clear`, `Encrypted`, `Signed`, or encrypted-and-signed.
     - Reading encrypted cards, and verifying or writing signed ones, needs the **user's private keyring**.
   - **Documented** ([Proton Contacts](https://proton.me/support/proton-contacts)):
     - display names and email addresses are not zero-access encrypted, so Proton can read them;
     - other fields (phone, address, notes and so on) are zero-access encrypted;
     - all contact data is signed.
   - **What a client needs.** **Verified in source** for Bridge's own login flow: a full Proton login, then the keys unlocked (Bridge keeps a key password, `KeyPass`, in its vault). That means the **account password** (SRP), **2FA**, sometimes human verification, and the key/mailbox password. The Bridge password is not enough.
   - **Official status.** **Not found:** any statement that this API is supported for third parties. The README calls the library "a client and development server for (a subset of) the Proton REST API". The project's own ROADMAP already calls it unofficial.
2. **Export and import in the web app. Documented** ([Proton Contacts](https://proton.me/support/proton-contacts)). Contacts can be exported as `.VCF` (decrypted in the browser) and imported from vCard or CSV. This is manual: there is no automatic sync.
3. **An address book built from mail headers over IMAP.**
   - **Inferred / unconfirmed** as a design, but each piece is **verified in source**: ENVELOPE (From/To/Cc/Bcc) comes from gluon's database without loading message bodies (section 9).
   - So a client can FETCH `ENVELOPE` over `Sent` (people the owner wrote to) and `INBOX` / `All Mail` (people who wrote), and rank names and addresses by how often and how recently they appear.
   - This gives names and addresses only. It's not the owner's real contact list and has no phone numbers or notes.

---

## What this means for proton-mail-mcp

**Date ordering (0.1).**

- `summaries` keeps "the highest UIDs" as the newest messages. On Bridge that's wrong (section 3):
  - after the initial sync the highest Proton IDs have the *lowest* UIDs;
  - later mail continues upward;
  - edited drafts and sent messages get new UIDs.
- The plan to UID SEARCH, then FETCH `UID INTERNALDATE` for all matches and sort client-side is the right fix.
  - It's cheap: INTERNALDATE is a database read with no literal load (section 9).
  - The cost grows with the number of matches, so cap it or fetch in chunks for very large folders.
  - Then fetch ENVELOPE/FLAGS only for the top N after sorting.
- Use UID as a tiebreaker only.
- **Match FETCH responses by UID, not by order.** They can arrive out of order. go-imap v2's `Collect` does this already.
- **Cache UIDs together with UIDVALIDITY.** A Bridge repair or address-mode switch changes UIDVALIDITY and makes every UID invalid. A tool given a stale UID should fail clearly rather than act on another message.

**Search (`search_messages`).**

- **Tell users what search can't do.** FROM/TO/SUBJECT/BODY are case-insensitive substring matches on *raw* bytes (section 2).
  - Non-ASCII terms (å, ä, ö) usually won't match subjects, which are RFC 2047 encoded, or bodies, which are quoted-printable or base64.
  - BODY can miss words split across quoted-printable lines, and text that only appears inside HTML-only or base64 parts.
  - Say this in the tool description so Claude doesn't conclude "no such mail".
- **One possible improvement.** For non-ASCII terms, run a coarse IMAP pre-filter on ASCII criteria and dates, then decode the candidates client-side with the existing `internal/mail` parser and match there. This costs a FETCH per candidate, so bound the candidate count.
- **Keep date criteria to SINCE/BEFORE.** They work on INTERNALDATE with UTC day boundaries, so ask for a day more on each side and cut the matches to the owner's local days by the date shown (what `SearchMessages` does). Avoid `SENT*` keys: one bad `Date:` header fails the whole SEARCH.
- **Be careful with BODY on large folders.** It loads every message, so keep the per-operation timeout and suggest narrowing by folder or date.
- **Results can be partial during the initial sync.**
- **SEARCH and FETCH under EXAMINE are fine.** EXAMINE never sets `\Seen`, which matches the "never marks read" guardrail.

**`trash_message` (0.2).**

- **Use `UID MOVE` to the mailbox with the `\Trash` attribute**, found by attribute rather than by the name "Trash". This calls the API's label-as-Trash, unlabels the source, and deletes nothing. Messages stay recoverable in Proton's Trash until it is emptied, or after 30 days if the owner has auto-delete on (section 6).
- **Never use STORE `\Deleted` + EXPUNGE.** In Trash and Drafts it is a **permanent delete** on Proton. Elsewhere it unlabels or does nothing. This supports the planned "no EXPUNGE" guardrail test with concrete evidence.
- **Know what MOVE does on Bridge** (section 4):
  - MOVE *to* `Labels/X` from a folder doesn't remove the message from that folder;
  - MOVE INBOX↔Sent, and anything into or out of All Mail or Scheduled, is refused;
  - unlabelling on folder moves is controlled by a remote Proton feature flag, so behaviour can change without a Bridge update.
- **`flag` / `unflag` map straight to Proton's Starred, and `mark_read` / `mark_unread` sync too.**
  - Don't offer custom keywords or `\Answered` as if they sync: they stay local and are lost on resync (section 5).
  - The read and flag operations need Proton to be reachable and fail cleanly when it isn't.

**Drafts (0.3).**

- **`create_draft`** = APPEND a complete RFC 822 message to the `\Drafts` mailbox. Bridge turns it into a real Proton draft and returns its UID in APPENDUID.
  - Bridge rebuilds the message.
  - Custom headers, and `In-Reply-To`/`References`, aren't stored on the draft (section 7).
  - The sender must be one of the account's addresses.
- **`create_reply_draft`.**
  - **Inferred / unconfirmed.** The threading headers won't survive APPEND, so Proton won't link the draft to its parent while it is a draft.
  - Threading can come back at send time only if the sent message carries the parent's `<ID@protonmail.internalid>` or external `Message-ID` in `References`. That's a reason for `send_draft` to rebuild these headers (see below).
  - Test this against a real Bridge before relying on it.
- **`update_draft`** can't edit in place. To change a draft:
  1. APPEND the new version.
  2. **MOVE the old one to Trash.** Changelog 3.23.1: drafts moved to Trash are not perma-deleted. Don't EXPUNGE it from Drafts, which deletes it permanently.
  3. Return the new UID.
  - Expect the UID to change whenever the owner edits the draft in another Proton app.
- **`list_drafts`.** Sort by INTERNALDATE as everywhere else.

**`send_draft` (0.6).**

- **There is no IMAP or SMTP command to "send draft X".** Bridge's SMTP path always creates a fresh draft and sends it (section 8).
- **The route this research points to:**
  1. Read the draft's literal from Drafts by UID. Re-check that it is in `\Drafts` and has `\Draft`.
  2. Build the SMTP envelope: `MAIL FROM` = the draft's From, which must be one of the account's addresses, and `RCPT TO` = every To, Cc and Bcc address. Bridge drops To entries that aren't also in RCPT.
  3. Submit it.
- **What happens to the original draft.** Bridge itself then **permanently deletes the original draft**, but only after a successful send. It finds the draft through the `<ID@protonmail.internalid>` entry that it put in the draft's `References`.
  - So "moves the draft away after sending" happens on Bridge's side, as a delete, not a move to Trash. Note that in the guardrail docs.
  - Our code still never issues EXPUNGE.
  - **Inferred / unconfirmed.** Test it end to end.
- **Strip Bridge's internal headers before submitting:** `X-Pm-Gluon-Id`, `X-Pm-Internal-Id`, `X-Pm-External-Id` and `X-Pm-Date`. Keep `References`.
  - **Inferred / unconfirmed.** Whether those headers would otherwise reach recipients. The send path builds packages from the parsed body, but this was not traced fully.
- **Don't APPEND to Sent.** Proton files the sent copy itself. Bridge only de-duplicates an APPEND within 90 s.
- **A repeat send within 90 s returns SMTP success without sending.** Treat that as success, which makes the tool idempotent, and don't loop on retries.
- **Expect some sends to be refused.** Three quick failures lock SMTP for 20 s. The body must be `text/plain` or `text/html`, with attachments.
- **Authenticate** with the account address and the Bridge password, over STARTTLS to localhost. Bridge allows AUTH before TLS, but we must not.

**Contacts (new).** The options, from safest to riskiest:

| Option | What it gives | Security trade-off |
|---|---|---|
| **A. Address book from mail headers** (FETCH ENVELOPE over Sent, INBOX and, if visible, All Mail; rank by frequency and recency) | Listing and searching people the owner corresponded with, by name or address. Filling a draft's `To` from a name. Read-only. | Uses only the existing IMAP connection, the Bridge password and no new dependency. Names and addresses come from email headers, which are **untrusted input**: a sender can claim any display name. Always show the resolved address and let the owner confirm before it goes into a draft. Can't update real Proton contacts and has no phone numbers or notes. |
| **B. Owner-exported vCard file** (a `.vcf` exported from the web app, read from a path the owner configures) | Real contact names and addresses, and other fields, offline. Read-only. | No new secret and no network. But the file is the owner's **decrypted** contacts at rest on disk: it must be protected and never committed. It goes stale until re-exported. Parsing vCard would need a vCard library (new dependency, needs approval) or a small hand-written parser. Updates are impossible. |
| **C. Proton API via go-proton-api** (list, search, create, update, delete contacts) | The full feature set, including updates. | A **new dependency** that needs approval, and an **unofficial** API with no third-party support statement. It needs the **account password, 2FA and the key password** in our process, far more power than the Bridge password: it can read all mail, change settings and delete data. It also means storing session tokens and handling human verification and CAPTCHAs. This breaks the current "Bridge password is the only secret" and "Bridge on localhost only" model. Treat it as a separate project with its own risk assessment (ROADMAP "Later"). |
| **D. Wait for Proton** (CardDAV, or contacts in Bridge) | Would be the clean answer. | Not available. No public plan found. |

The suggestion that fits the project's rules is **A**: read-only, no new secret, no new dependency, with a confirm step whenever a name is resolved to an address for a draft. **B** could be added later as an opt-in, with owner approval for any parser dependency. **C** shouldn't be done under the current guardrails.

---

## Sources

Source code (all at fixed commits):

- gluon `7e80097`: <https://github.com/ProtonMail/gluon/tree/7e800978ab4a9942829eb6116e33ea0278c03e91>
  - `imap/capabilities.go`, `internal/session/session.go`, `internal/session/handle_search.go`, `internal/session/handle_append.go`, `internal/session/handle_store.go`
  - `imap/command/search.go`, `imap/command/date_time.go`
  - `internal/state/mailbox_search.go`, `internal/state/mailbox_fetch.go`, `internal/state/mailbox.go`, `internal/state/updates.go`, `internal/state/actions.go`, `internal/state/state.go`
  - `internal/backend/connector_updates.go`, `internal/db_impl/sqlite3/write_ops.go`, `internal/db_impl/sqlite3/v1/mailbox.go`
  - `imap/uid_validity_generator.go`, `rfc822/header.go`, `internal/ids/header.go`, `internal/ids/ids.go`, `builder.go`
- proton-bridge `36d7b6a`: <https://github.com/ProtonMail/proton-bridge/tree/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2>
  - `internal/services/imapservice/connector.go`, `helpers.go`, `imap_updates.go`, `utils.go`, `sync_build.go`, `sync_update_applier.go`, `service.go`, `service_message_events.go`
  - `internal/services/syncservice/stage_metadata.go`, `stage_download.go`
  - `internal/services/smtp/smtp.go`, `smtp_prefs.go`, `smtp_backend.go`, `accounts.go`
  - `internal/services/imapsmtpserver/smtp.go`, `imap.go`, `service.go`
  - `internal/services/useridentity/state.go`, `pkg/message/build.go`, `Changelog.md`
- go-proton-api `8a62023`: <https://github.com/ProtonMail/go-proton-api/tree/8a62023d3e38cb29749bcfc01bf44eafda6cec82>
  - `message.go`, `message_import.go`, `contact.go`, `contact_types.go`, `contact_card.go`, `README.md`
- ProtonMail/go-smtp `2b3d505`: <https://github.com/ProtonMail/go-smtp/tree/2b3d50599865839dfccb6bf4805996fc3d476f29>
  - `server.go`, `conn.go`

Proton documentation and other pages:

- Labels in Bridge: <https://proton.me/support/labels-in-bridge>
- Recovered Messages folder: <https://proton.me/support/what-is-the-recovered-messages-folder-in-bridge>
- Auto-delete unwanted messages: <https://proton.me/support/auto-delete-unwanted-messages>
- Proton Contacts (encryption, export/import): <https://proton.me/support/proton-contacts>
- Bridge release notes (early releases): <https://proton.me/download/bridge/early_releases.html>
- gluon issue #378 (RFC 5256 SORT/THREAD): <https://github.com/ProtonMail/gluon/issues/378>
- Proton UserVoice, DAV support for contacts and calendar: <https://protonmail.uservoice.com/forums/932842-proton-calendar/suggestions/45730093-dav-support-for-contacts-calendar>
