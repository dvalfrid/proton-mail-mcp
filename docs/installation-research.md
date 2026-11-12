# Installing proton-mail-mcp: research

Researched 2026-11-11. Scope: facts needed for a cross-platform `setup` wizard, a `check` command and packaging. IMAP behaviour already covered in `docs/bridge.md` (branch `docs/bridge-behaviour`) is not repeated.

**Labels**

- **Verified in source**: read in code at the commit linked. Commits used:

  | Repo | Commit |
  |---|---|
  | ProtonMail/proton-bridge | `36d7b6a` (2026-11-06, = remote HEAD, 3.27.2+git) |
  | ProtonMail/gluon | `7e80097` (the commit Bridge pins) |
  | ProtonMail/go-smtp | `2b3d505` (Bridge's fork) |
  | zalando/go-keyring | `66b55cc` (after tag v0.2.8) |
  | modelcontextprotocol/mcpb | `70fe3b3` (= remote HEAD, package 2.1.2) |
  | golang/sys | `6f4195b` (2026-11-10) |
  | dvalfrid/rigstats | `9189009` |
  | dvalfrid/proton-mail-mcp | `801838c` (local, read only) |
  | microsoft/winget-pkgs (Bridge manifest) | `0d87e8d` |

- **Verified (observed)**: seen on one Windows 11 machine running Proton Mail Bridge 3.27.1 and the MSIX build of Claude Desktop; read-only checks (registry, file names, a TCP greeting, no login). Not seen on macOS or Linux.
- **Documented**: official docs, linked.
- **Inferred / unconfirmed**: says what would confirm it.

---

## A. Bridge on each OS

### A1. Where Bridge is installed

| OS | Fact | Label |
|---|---|---|
| All | Three executables: launcher `proton-bridge`, core `bridge` (owns the IMAP/SMTP ports), GUI `bridge-gui`; `.exe` on Windows ([Makefile L23, L73-L82][b-make], [launcher main.go L51][b-launcher]). | Verified in source |
| All | The launcher prefers a newer build from the **updates dir** (`<UserConfigDir>/protonmail/bridge-v3/updates/<version>/` on Windows/macOS; `~/.local/share/protonmail/bridge-v3/updates` on Linux), verified with a PGP `.sig`; otherwise the binary next to the launcher ([launcher L125-L150][b-launcher], [versioner][b-versioner]). So the process holding port 1143 can live in a **user-writable** directory. | Verified in source |
| Windows | Install dir `C:\Program Files\Proton AG\Proton Mail Bridge\`, containing `proton-bridge.exe`, `bridge.exe`, `bridge-gui.exe`, `crashpad_handler.exe`. The running core was `...\Proton Mail Bridge\bridge.exe`. | Verified (observed) |
| Windows | Proton's CLI guide gives the same path: `"%programfiles%\Proton AG\Proton Mail Bridge\proton-bridge.exe" -c` ([Bridge CLI guide][d-cli]). The source's fallback path `C:/Program Files/Proton/Proton Mail Bridge` ([locations.go L83][b-locations]) is stale. | Documented |
| Windows | Uninstall registry: machine-scope MSI made with Advanced Installer. Two keys under `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\`: `{75693F0A-D0D8-4DAF-9825-06A3859E7F0D}` (MSI product code, `WindowsInstaller=1`) and `Proton Mail Bridge 3.27.1` (Advanced Installer bootstrapper). `DisplayName = Proton Mail Bridge`, `Publisher = Proton AG`, `InstallLocation` = the dir above. | Verified (observed) |
| Windows | Whether the product code GUID is stable across versions: **not confirmed**. Detect by `DisplayName`+`Publisher`, not by GUID. Checking the key after an upgrade would confirm. | Inferred / unconfirmed |
| Windows | winget: `Proton.ProtonMailBridge`, `InstallerType: exe`, `Scope: machine`, silent switch `/exenoui /quiet`, installer `Bridge-Installer.exe` from GitHub releases ([winget manifest][w-bridge]). | Verified in source |
| macOS | Bundle `/Applications/Proton Mail Bridge.app` ([locations.go L116][b-locations]); launcher inside is `Contents/MacOS/Proton Mail Bridge` ([CLI guide][d-cli]). Homebrew cask `proton-mail-bridge` ([formulae.brew.sh][d-cask]). | Verified in source / Documented |
| macOS | Bundle ID: third-party sources disagree: `com.protonmail.bridge` (Fleet, PatchBee, MacUpdater) vs `ch.protonmail.bridge` (fullmetalmac). Not in Bridge's public source. Confirm with `mdls -name kMDItemCFBundleIdentifier "/Applications/Proton Mail Bridge.app"`. | Inferred / unconfirmed |
| Linux | Proton ships `.deb` and `.rpm` ([verifying package][d-verify]). Arch `extra`: `protonmail-bridge` (GUI) and `protonmail-bridge-core`; files `/usr/lib/protonmail/bridge/bridge`, `/usr/lib/protonmail/bridge/bridge-gui`, `/usr/bin/protonmail-bridge`, `/usr/bin/protonmail-bridge-core`, and a systemd **user** unit `protonmail-bridge.service` ([Arch file list][d-arch]). Desktop file `Exec=protonmail-bridge` ([dist/proton-bridge.desktop][b-desktop]). | Documented |
| Linux | Proton's .deb/.rpm use the same `/usr/lib/protonmail/bridge/` layout. Confirm with `dpkg -L protonmail-bridge`. | Inferred / unconfirmed |
| Linux | Flatpak `ch.protonmail.protonmail-bridge` is **community**, "not verified by, affiliated with, or supported by Proton AG"; x86_64 only; wraps Proton's .deb ([Flathub][d-flathub]). Its data dir is under `~/.var/app/ch.protonmail.protonmail-bridge/` (Flatpak convention; unconfirmed). | Documented / Inferred |

### A2. "Not installed" vs "installed but not running"

- **Verified in source.** Bridge listens only on `127.0.0.1` ([constants.go L71][b-const], [listener.go L28][b-listener]). Not `::1`: connect to `127.0.0.1`, not `localhost`.
- **Verified in source.** A single-instance lock: `<UserCacheDir>/protonmail/bridge-v3/bridge-v3.lock`, holding the PID as text ([singleinstance.go L37][b-single], [migration.go getPID][b-migration]). The GUI has `bridge-v3-gui.lock`. A stale file can survive a crash, so its existence alone is weak evidence.
- **Recommended order (Inferred / unconfirmed as a design, each step's facts verified above):**
  1. Probe `127.0.0.1:<port>`: connection + IMAP greeting matching section B → "running".
  2. If refused: look for an install. Windows: uninstall key with `DisplayName = Proton Mail Bridge`, `Publisher = Proton AG`. macOS: `/Applications/Proton Mail Bridge.app` (or Spotlight by bundle ID once known). Linux: `/usr/lib/protonmail/bridge/bridge`, `/usr/bin/protonmail-bridge*`, `flatpak info ch.protonmail.protonmail-bridge`. Found → "installed, not running" (message: start Bridge; on Linux possibly `systemctl --user start protonmail-bridge`).
  3. Neither, but `<UserConfigDir>/protonmail/bridge-v3/vault.enc` exists → "was set up here, binary not found" (portable/custom install).
  4. Nothing → "not installed", with the download link.
  5. Port open but greeting not Bridge's → "something else holds the port": hard error.

### A3. Config and data dirs; where the TLS certificate lives

- **Verified in source.** Paths: `provider = os.UserConfigDir()/protonmail/bridge-v3`, data and cache likewise ([provider.go L41][b-provider], [app.go L474][b-app], [constants.go L28/L65][b-const]). Created `0o700`.

  | | Windows | macOS | Linux |
  |---|---|---|---|
  | Settings (vault) | `%APPDATA%\protonmail\bridge-v3` | `~/Library/Application Support/protonmail/bridge-v3` | `~/.config/protonmail/bridge-v3` |
  | Data (gluon, logs, updates) | same as settings | same as settings | `~/.local/share/protonmail/bridge-v3` |
  | Cache (lock files) | `%LOCALAPPDATA%\protonmail\bridge-v3` | `~/Library/Caches/protonmail/bridge-v3` | `~/.cache/protonmail/bridge-v3` |

  **Verified (observed)** on Windows: `vault.enc`, `gluon`, `imap-sync`, `logs`, `updates`, `grpcFocusServerConfig.json` in the settings dir; `bridge-v3.lock`, `bridge-v3-gui.lock` in the cache dir.
- **Verified in source. The certificate is inside the encrypted vault, not a file.** `vault.Certs.Bridge{Cert, Key}` holds PEM bytes; generated with the default vault data ([types_certs.go][b-typescerts], [types_data.go L32][b-typesdata]). `vault.enc` is AES-256-GCM, key = SHA-256 of a random key stored in the OS keychain under `bridge-v3` / `bridge-vault-key` ([vault.go L64-L76][b-vault], [helper.go L29][b-vaulthelper]). If the keychain fails, Bridge uses an **unencrypted-key "insecure" vault** in `<settings>/insecure/` ([app/vault.go L78][b-appvault]).
- **Verified in source.** Format: self-signed X.509, RSA-2048, PEM `CERTIFICATE` and PKCS#1 `RSA PRIVATE KEY` ([certs/tls.go L40-L90][b-certs]).
- **Verified in source.** The only on-disk `cert.pem` is legacy: Bridge v2 kept `<config>/protonmail/bridge/cert.pem`, which v3 migrates into the vault ([migration.go L110][b-migration]).
- **Verified in source.** Optional custom cert: "Import TLS certificate" stores `CustomCertPath`/`CustomKeyPath`; if set and readable, that cert is served instead ([vault/certs.go][b-vaultcerts]).
- **Verified in source. "Export TLS certificates"** (GUI gRPC `ExportTLSCertificates`, CLI `exportTLSCerts`) writes **both `cert.pem` and `key.pem`, mode `0600`**, into a folder the user picks ([service_cert.go L63][b-svccert], [cli/system.go L288][b-clisystem]). So today's manual instruction leaves Bridge's **private key** on disk. On Windows `0600` maps to little; the file inherits the folder's ACL (Inferred).
- **Verified in source.** On macOS only, Bridge can install the cert as a trusted root in the user's login keychain (for Apple Mail); Windows and Linux installers are no-ops ([cert_store_darwin.go][b-certdarwin], [cert_store_windows.go][b-certwin]).
- **Verified in source.** `GetTLSConfig` does not exist in this tree. The IMAP/SMTP TLS config is `loadTLSConfig`: `tls.X509KeyPair(vault.GetBridgeTLSCert())`, `MinVersion: TLS 1.2` ([bridge.go L652][b-bridge]). `certs.GetConfig` (with an "expires soon" check) has no callers.

### A4. Regeneration and lifetime

- **Verified in source.** Lifetime **20 years** (`NotAfter = now + 20*365*24h`), `IsCA: true`, `CommonName "127.0.0.1"`, IP SAN `127.0.0.1` ([certs/tls.go L40][b-certs]). **Verified (observed):** notBefore 2026-11-11, notAfter 2046-11-06.
- **Verified in source.** No renewal path: the "expires soon" check is dead code (A3). The cert changes only when the vault's default data is recreated:
  - new vault (first run, or `vault.enc` deleted);
  - corrupt vault, which is reset ([app/vault.go][b-appvault]);
  - keychain unavailable → separate insecure vault → new cert;
  - **Factory reset** (`vault.Reset` → `newDefaultData`) ([settings.go L320][b-settings]);
  - the user imports a custom cert.
- **Inferred / unconfirmed.** A reinstall that keeps `%APPDATA%\protonmail` keeps the cert; whether the Windows/macOS uninstallers delete that dir is not in the public source. Uninstall + reinstall on a test machine would confirm.
- Consequence for us: a pinned fingerprint is long-lived, but `check` must say clearly "Bridge's certificate changed (factory reset / reinstall?)" and point to `setup`.

### A5. Ports

- **Verified in source.** Defaults are chosen when the vault is created: `FindFreePortFrom(1143)` and `FindFreePortFrom(1025, imapPort)` ([types_settings.go L76][b-settings-types]). So if 1143 was busy at first run, Bridge picked **another port**. The user can change ports and switch STARTTLS ↔ implicit SSL (`IMAPSSL`, `SMTPSSL`, default false) in the GUI (`SetMailServerSettings`).
- **Verified in source.** The chosen ports live **only in the encrypted vault** (`Settings.IMAPPort/SMTPPort`). No plain config file.
- **Verified (observed):** 1143 and 1025 on 127.0.0.1, plus two random high ports (gRPC).
- So setup must ask (default 1143/1025), or read them from the user's Bridge window, and `check` must treat "refused on 1143" as possibly "Bridge uses another port".

### A6. CLI or local API

- **Verified in source.** `--cli`/`-c` runs an interactive shell (ishell) frontend; flags also include `--noninteractive`, `--grpc`, `--version` ([app.go L58-L97][b-app]). It is a **second Bridge instance**: it hits the single-instance lock and exits if the running one is not older ([singleinstance.go][b-single]). So it can't query a running GUI Bridge. Not usable by us.
- **Verified in source.** The GUI talks to the core over **gRPC** on `127.0.0.1:0` (Windows) or a Unix socket (macOS/Linux), TLS with a fresh cert, plus a per-run UUID token. Port, cert and token are written to `<settings>/grpcServerConfig.json` with `os.Create` (default 0666 & umask; no explicit mode) ([service.go L119-L160][b-grpc], [service/config.go][b-svcconfig]). Methods include `MailServerSettings` (ports) and `ExportTLSCertificates`.
- **Verified (observed).** After the GUI started, `grpcServerConfig.json` was **not present** (only `grpcFocusServerConfig.json`), so the GUI seems to delete it after reading. Inferred / unconfirmed; reading `bridge-gui` source (`internal/frontend/bridge-gui`) would confirm.
- **Assessment (Inferred).** Using that private, undocumented, all-powerful API (it can log accounts in and out) would be fragile and would hand our tool far more power than it needs. Same for reading Bridge's vault key from the keychain and decrypting `vault.enc`. **Don't.**

---

## B. Recognising Bridge on the wire

### B1. Greetings

- **IMAP greeting. Verified in source + observed.** `* OK [CAPABILITY ...] <Name> <Version> - gluon session ID <n>` ([session.go L345][g-greet]); Bridge sets Name to `FullAppName` = `Proton Mail Bridge` (Makefile `APP_FULL_NAME`) ([imap.go L193][b-imap]). Observed:
  `* OK [CAPABILITY AUTH=PLAIN ID IDLE IMAP4rev1 MOVE STARTTLS UIDPLUS UNSELECT] Proton Mail Bridge 03.27.01 - gluon session ID 6`
  Note the zero-padded version and that the greeting's capability list includes MOVE/UIDPLUS/UNSELECT before login.
- **ID response. Verified in source + observed.** `* ID ("name" "Proton Mail Bridge" "version" "03.27.01" "os" "windows" "vendor" "TODO" "support-url" "TODO")` (vendor and support URL are literally `"TODO"` in [imap.go L193][b-imap]).
- **SMTP banner. Verified in source + observed.** `220 127.0.0.1 ESMTP Service Ready` ([smtp.go L49][b-smtp], [go-smtp conn.go greet][s-greet]); EHLO lists `STARTTLS`, `AUTH PLAIN LOGIN`, `8BITMIME`, `CHUNKING`, `SIZE`. Generic: not identifying.
- **Inferred.** All of this is plain text before STARTTLS: any process can fake it. It is useful for **error messages** ("port 1143 is not Bridge"), not for trust.

### B2. Certificate subject

- **Verified in source + observed.** Subject = Issuer = `C=CH, O=Proton AG, OU=Proton Mail, CN=127.0.0.1`, self-signed, CA:true, random 128-bit serial ([certs/tls.go L40][b-certs]).
- **Inferred.** Distinctive but trivially forgeable (anyone can make a self-signed cert with that subject). A useful sanity check; not proof. A user-imported custom cert would not match it (warn, don't fail).

---

## C. Which process owns the port (Go, per OS)

| OS | How, without cgo | Label |
|---|---|---|
| Windows | `GetExtendedTcpTable(TCP_TABLE_OWNER_PID_LISTENER)` is **not** in `golang.org/x/sys/windows` at `6f4195b` (grep: absent); call it via `windows.NewLazySystemDLL("iphlpapi.dll").NewProc(...)`. Then `OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION)` + `QueryFullProcessImageName` ([zsyscall L3099][x-zsys]). | Verified in source |
| Windows | Authenticode without cgo: `WinVerifyTrustEx` + `WINTRUST_ACTION_GENERIC_VERIFY_V2` + `WinTrustData`/`WinTrustFileInfo` exist in x/sys ([zsyscall L4499][x-zsys], [types_windows.go][x-types]); `CryptQueryObject` (L1562) and `CertGetNameString` exist for reading the signer. `CryptMsgGetParam` / `WTHelperProvDataFromStateData` are not wrapped → LazyDLL. | Verified in source |
| Windows | The Bridge binaries are signed `CN=Proton AG, O=Proton AG, S=Genève, C=CH` (EV-style subject, serial `CHE-354.686.492`), status Valid. | Verified (observed) |
| Windows | `GetExtendedTcpTable` with owner PID needs no admin; `PROCESS_QUERY_LIMITED_INFORMATION` on a same-user process normally succeeds. | Inferred / unconfirmed (test as non-admin) |
| macOS | No pure-Go API for socket→PID: `proc_pidinfo`/`proc_pidfdinfo` are libproc (cgo or `go:linkname` tricks, which Go 1.23+ restricts). Practical: exec `/usr/sbin/lsof -nP -iTCP@127.0.0.1:1143 -sTCP:LISTEN -Fp`. Exe path: `unix.SysctlRaw("kern.procargs2", pid)` (no cgo); `SysctlKinfoProc` gives only a 16-char name ([syscall_darwin.go L503][x-darwin]). | Inferred (APIs verified in x/sys; lsof flags from its man page) |
| macOS | Signature: exec `codesign --verify --strict <app>` and `codesign -dv --verbose=4` → `TeamIdentifier`. Proton AG's Team ID: **not found** in public sources. Confirm on a Mac with the command above. | Inferred / unconfirmed |
| Linux | `/proc/net/tcp` (hex `0100007F:0477` for 127.0.0.1:1143, state `0A` = LISTEN) → inode → scan `/proc/<pid>/fd/*` for `socket:[inode]` → `readlink /proc/<pid>/exe`. Works for same-user processes without root. | Inferred (standard procfs; not tested here) |
| Linux | Flatpak: Bridge shares the host network namespace (`--share=network`), so the socket shows in `/proc/net/tcp`, but `/proc/<pid>/exe` resolves to a path inside the sandbox (`/app/...`), not a host path. Linux packages have no signature on the binary to check. | Inferred / unconfirmed |

**Is it worth it? (Inferred, my view.)** Mostly no, as a hard gate:

- What it defends against: another local process grabbing `127.0.0.1:1143` **at setup time** (before we pin). After pinning, an impostor cannot present Bridge's certificate: the private key is only in Bridge's vault. So pinning already gives the protection afterwards.
- A same-user attacker gains nothing from beating the pin: it can already read Bridge's vault key from the user's keychain (Windows Credential Manager has no per-app ACL) or our own stored password. Process checks don't stop it.
- The real residual threat is a **different local user** (shared machine) or a non-Bridge app squatting the port while Bridge is stopped. TOFU + showing the fingerprint + B1/B2 sanity checks + "Bridge must be running and you must see it in the tray" catches the common accidents.
- The cost is high and uneven: three OS-specific code paths, exec of `lsof`/`codesign` on macOS, no signature on Linux packages, user-writable update dirs, a Team ID we can't confirm, and flatpak paths that don't resolve.
- **Cheap middle ground:** on Windows only (where everything is in x/sys or one LazyDLL, and the Proton signer is known), `setup` and `check` can *report* "port owned by `bridge.exe`, signed by Proton AG" or warn when it isn't. Treat it as an advisory signal, not a gate.

---

## D. OS credential store from Go

### D1. zalando/go-keyring

- **Windows. Verified in source.** Via `github.com/danieljoos/wincred` (Credential Manager, generic credential); password limit **2560 bytes** ([keyring_windows.go L30][k-win]). Any same-user process can read generic credentials (Inferred; Windows has no per-app ACL there).
- **macOS. Verified in source.** Shells out to `/usr/bin/security`:
  - `Set`: starts `security -i` and writes `add-generic-password -U -s … -a … -w <base64 secret>` to **stdin**, not argv. So the secret is **not** in the process list ([keyring_darwin.go L70][k-darwin]). Limit: whole command ≤ 4096 bytes (comment says ~3000 bytes in all).
  - `Get`: `security find-generic-password -s <svc> -wa <user>`; the secret comes back on stdout, not argv.
- **macOS caveat. Documented in issues, not by zalando.** Because `/usr/bin/security` creates the item, the item's ACL trusts `security` itself, so **any same-user process can read it with `security find-generic-password -w` without a prompt** ([branow/atlassian-cli#20][k-acl]). Upstream fix PR [#158][k-158] (opt-in Security-framework backend) is **open, not merged**. Open bug [#153][k-153]: a failing `Set` can return nil at the maximum command length.
- **Linux. Verified in source.** Secret Service over the D-Bus **session bus** (`godbus/dbus/v5`), `OpenSession("plain")` (unencrypted transfer over the local bus), items in the `login` collection with attributes `service`/`username`, unlocking it if needed ([keyring_unix.go][k-unix], [secret_service.go L73][k-ss]). Build tag: linux always; *BSD only with cgo.
- **Headless Linux. Inferred / unconfirmed.** With no session bus or no `org.freedesktop.secrets` provider, `Set`/`Get` return a D-Bus error (no fallback in the code). With a bus but a locked/absent `login` collection, `Unlock` may trigger a prompt or fail. Test on a server without gnome-keyring.
- **Dependencies. Verified in source.** `github.com/danieljoos/wincred v1.2.3`, `github.com/godbus/dbus/v5 v5.2.2`, indirect `golang.org/x/sys` ([go.mod][k-gomod]). Licence MIT. Latest tags v0.2.7 (2026-03-20), v0.2.8 (2026-03-23); repo active (pushed 2026-10); ~1.4k stars.
- **Advisories.** None for `zalando/go-keyring` in the GitHub Advisory Database (API query returned 0). Verified (observed via API).
- **Size limits.** Windows 2560 B password; macOS ~3000-4096 B total; Linux none in principle ([keyring.go L16-L20][k-keyring]). A Bridge password (16 chars) is far below all of them.

### D2. Alternatives

| Option | Trade-offs | Label |
|---|---|---|
| `99designs/keyring` | Many backends (Keychain, WinCred, Secret Service, KWallet, pass, encrypted file). macOS backend uses `keybase/go-keychain` (**cgo**), which breaks easy cross-compilation from Linux CI. Last release v1.2.2 (2022-12), last push 2024-05: low maintenance. MIT. | Verified (GitHub API) / Inferred (cgo from its README) |
| Bridge's own approach | `docker/docker-credential-helpers` (Windows, Secret Service via cgo/libsecret) + `keybase/go-keychain` (Proton fork) + `pass` + raw godbus ([proton-bridge go.mod][b-gomod], [pkg/keychain][b-keychain]). Heavy, cgo. | Verified in source |
| Own minimal code | Windows: `CredWriteW`/`CredReadW` via x/sys LazyDLL (x/sys is already an indirect dependency). macOS: Security framework needs cgo or `purego`. Linux: godbus. Most code to own. | Inferred |
| No keychain: `0600` config file | Simplest; same protection as go-keyring on macOS today (same-user readable); no headless problem. Weaker on Windows if the dir ACL is broad. | Inferred |
| Env var from the MCP client | What MCPB does (F). Secret visible in the client's config/env. | Documented |

---

## E. Claude client registration

- **Paths. Documented** ([MCP: connect local servers][d-mcplocal]):
  - macOS `~/Library/Application Support/Claude/claude_desktop_config.json`
  - Windows `%APPDATA%\Claude\claude_desktop_config.json`
  - Linux `~/.config/Claude/claude_desktop_config.json` (the page says Claude Desktop is available for Linux).
- **Windows MSIX. Verified (observed) + Documented in issues.** The Store/MSIX build has package family `Claude_pzs8sxrjxfjjc` and a virtualised dir `%LOCALAPPDATA%\Packages\Claude_pzs8sxrjxfjjc\LocalCache\Roaming\Claude\`; on the observed machine `%APPDATA%\Claude\` did not exist. Issues [#25579][e-25579] and [#26073][e-26073] report the app reads the packaged copy while "Edit Config" opens the other one. Which file a given build reads: **not officially documented**. Confirm by writing a test server into one file and seeing it in Claude Desktop.
- **Windows Store build 5.1231. Verified (observed, issue #42).** This build keeps its config in `%LOCALAPPDATA%\Claude-Data\claude_pzs8sxrjxfjjc\claude_desktop_config.json` (the app writes its own preferences there) and does not start a server listed only in the `LocalCache` copy. setup and check therefore use one shared list (`config.ClaudeDesktopConfigs`) with all three locations, and setup offers each one whose directory exists.
- **JSON. Documented.**
  ```json
  { "mcpServers": { "proton-mail": {
      "command": "C:\\path\\proton-mail-mcp.exe",
      "args": [],
      "env": { "KEY": "value" } } } }
  ```
  Absolute paths required; restart Claude Desktop after editing; server stderr goes to `mcp-server-<name>.log` in the Claude logs dir ([MCP docs][d-mcplocal]).
- **Claude Code. Documented** ([Claude Code MCP docs][d-ccmcp]): `claude mcp add [options] <name> -- <command> [args...]`, `--env KEY=value` (put another option such as `--transport stdio` between `--env` and the name), `-s/--scope local|project|user`. Local (default) and **user** scope are stored in **`~/.claude.json`**; project scope in `.mcp.json` in the repo root. `claude mcp add-json <name> '<json>'` takes the inner object.
- **Inferred.** Registering via `claude mcp add` (exec of the user's `claude`) is safer than editing `~/.claude.json` ourselves (that file holds much other state). For Claude Desktop, edit JSON with a backup, a merge (never overwrite other servers) and an atomic rename.

---

## F. MCPB (Claude Desktop Extensions)

- **Manifest. Verified in source** ([MANIFEST.md][m-manifest], [schema][m-schema]): latest schema `manifest_version` `0.3` (a `0.4` schema adds `server.type: "uv"`). `server.type` ∈ `node | python | binary`; `server.entry_point`; `server.mcp_config` `{command, args, env, platform_overrides}`; `compatibility.platforms` ∈ `darwin | win32 | linux`.
- **Binary.** "For binaries, apps will automatically append `.exe` on Windows" (MANIFEST.md). Per-platform binaries go in one bundle, chosen with `platform_overrides` (keys `win32`, `darwin`, `linux`), e.g. `server/proton-mail-mcp-darwin-arm64`. Verified in source. Picking by architecture (arm64 vs x64) is not covered by `platform_overrides` (keys are OS only): ship a macOS universal binary, or a small arch dispatch. Inferred.
- **user_config.** Types `string|number|boolean|directory|file`; `required`, `default`, `multiple`, `min/max`, and `sensitive` ("mask input and store securely"). Values reach the server **only by substitution** of `${user_config.KEY}` into `args` or `env` at launch. `${__dirname}` = extension dir. Verified in source.
- **Where sensitive values are stored. Documented:** Anthropic says sensitive values go to the OS secret store, "Keychain on macOS, Credential Manager on Windows" and the distro keychain on Linux ([support article][d-support], [Desktop Extensions post][d-dxt]). Open issue [mcpb#244][m-244] (2026-05): Claude Desktop shows sensitive values **in plain text** in the extension's environment-variables panel.
- **Inferred.** At runtime the secret is an environment variable of our process: readable by same-user processes (e.g. `/proc/<pid>/environ` on Linux, debuggers elsewhere), comparable to a same-user keychain read.
- **CLI. Verified in source** ([CLI.md][m-cli]): `mcpb init`, `mcpb validate`, `mcpb pack`, `mcpb sign [--cert --key --intermediate | --self-signed]`, `mcpb verify`, `mcpb unsign`, `mcpb info`. Signature: DER PKCS#7 SignedData appended after the ZIP between `MCPB_SIG_V1` and `MCPB_SIG_END`.
- **Not found:** whether Claude Desktop enforces or displays `.mcpb` signatures, and which CAs it trusts. Test by installing a self-signed and an unsigned bundle.
- **Security caveats (Inferred).** The bundle runs with the user's full rights; an update replaces the binary silently; the Bridge password ends up in Claude Desktop's store rather than ours (one fewer store, but subject to #244); macOS binaries inside the bundle still need signing/notarisation if the `.mcpb` itself was quarantined (behaviour of extracted files: unconfirmed).

---

## G. Packaging and signing

### G1. Windows signing: reuse the rigstats pattern (per owner)

**What rigstats does. Verified in source** ([release.yml][r-release], [winget-submit.yml][r-winget], [docs/release.md][r-docs]):

- Jobs split by privilege. `build` has no secrets and a read-only token. `sign` runs in environment **`release-signing`**, which needs the owner's approval and alone holds `AZURE_TENANT_ID`, `AZURE_CLIENT_ID`, `AZURE_CLIENT_SECRET`. `publish` alone may write to the release.
- `Azure/artifact-signing-action` v2.0.0 (pinned by SHA). Settings: endpoint `https://neu.codesigning.azure.net/`, account `rigstats-codesign`, profile `RIGStats`, SHA256 file digest, RFC 3161 timestamp `http://timestamp.acs.microsoft.com`, then `Get-AuthenticodeSignature` must be `Valid`.
- The service principal is app registration `rigstats-github-action` with role **Artifact Signing Certificate Profile Signer on the whole account**; auth is a **client secret** (24 months).
- Winget: `wingetcreate update Codeby.RIGStats --version … --urls "<url>|x64|machine" --token $WINGET_TOKEN --submit`. `wingetcreate.exe` pinned by tag + SHA-256; skip if an update PR is already open; a `winget: hold` label on the release PR suppresses submission. `WINGET_TOKEN` is a classic PAT (`public_repo`, 90 days).

**proton-mail-mcp today. Verified in source** (`.github/workflows/release.yml`, `release-please.yml` at `801838c`): release-please creates tag + release with `GITHUB_TOKEN`, then dispatches `release.yml` with `inputs.tag`. `release.yml` builds 6 targets (windows/darwin/linux × amd64/arm64) on ubuntu in one job, zips Windows / tars others with LICENSE, README, `.env.example`; `publish` writes `SHA256SUMS.txt`, a build-provenance attestation and uploads with `gh release upload --clobber`. No signing.

**Installer or not? (Inferred, recommendation.)** No installer. A winget **zip + portable** package fits a CLI that Claude launches by absolute path:

| | Portable zip in winget | Small installer (NSIS/MSI) |
|---|---|---|
| What's signed | `proton-mail-mcp.exe` inside the zip | the exe and the installer |
| Install location | winget's per-user package dir (`%LOCALAPPDATA%\Microsoft\WinGet\Packages\…`, user scope) | `Program Files` (machine) |
| PATH | Symlink in the WinGet `Links` folder named by `PortableCommandAlias`; with `ArchiveBinariesDependOnPath: true` the folder is put on PATH instead ([installer schema][w-schema]) | installer edits PATH |
| `Commands` | Metadata only: "The Windows Package Manager does not support any behavior related to commands" ([schema][w-schema]) | same |
| Upgrade | `winget upgrade` swaps the dir; the Links symlink path stays stable | needs version-proof path |
| Known issues | zip+alias bugs ([winget-cli#6604][w-6604], [#2498][w-2498]); missing alias → folder appended to PATH ([beads#4908][w-beads]) | more code, UAC, uninstall logic |
| Fits Claude config | Register the **Links** path (stable across upgrades) or the resolved exe path | `C:\Program Files\…\proton-mail-mcp.exe` |

  Install path for portables and the Links folder (`%LOCALAPPDATA%\Microsoft\WinGet\Links`) are not stated in the schema page; they come from winget-cli issues. Inferred / unconfirmed; `winget install` of a test package would confirm.

  Manifest sketch (Inferred from the schema): `InstallerType: zip`, `NestedInstallerType: portable`, `NestedInstallerFiles: [{RelativeFilePath: proton-mail-mcp_<v>_windows_amd64\proton-mail-mcp.exe, PortableCommandAlias: proton-mail-mcp}]`, x64 and arm64 installers. `wingetcreate update <Id> --urls "<zip-x64>|x64" "<zip-arm64>|arm64"` (exact multi-URL syntax: check `wingetcreate update --help`).

**Signing account: shared vs separate (Inferred, recommendation).**

- *Same account, same profile, same service principal:* zero setup. But one leaked secret signs for both projects, and any rigstats workflow could sign proton-mail-mcp binaries and vice versa. rigstats' updater trusts its own signer subject, so it gains nothing from separation there; Windows users of either product see the same publisher anyway.
- *Same account + profile, separate app registration per repo* (**recommended**): the publisher identity (validated company identity) stays the same, but each repo has its own revocable principal. Scope the role to the **certificate profile** rather than the account; community guides do this, while the [action's OIDC doc][a-oidc] shows account/RG/subscription scope. Use a **federated credential (OIDC)** with subject `repo:dvalfrid/proton-mail-mcp:environment:release-signing`, so there is **no client secret** to rotate or leak ([artifact-signing-action OIDC][a-oidc]). Cost of a second profile: not researched (owner asked to skip pricing).

**Fitting it into the release flow (Inferred, recommendation).**

1. `build` (unchanged, no secrets) uploads **unsigned** Windows exes as an artifact before zipping.
2. New `sign-windows` job on `windows-latest`, `environment: release-signing`, `permissions: id-token: write, contents: read`. Steps: `azure/login` (OIDC), `Azure/artifact-signing-action` (SHA-pinned), check `Get-AuthenticodeSignature` = Valid, then zip.
3. `publish` takes signed zips + other archives, then checksums, attestation and upload as today. Attestation then covers the signed bytes.
4. `winget` job (or dispatched `winget-submit.yml` as in rigstats) after publish, skipped for pre-releases; honour a `winget: hold` label. Prefer a fine-grained token limited to the owner's `winget-pkgs` fork, if wingetcreate accepts one (unconfirmed); else the same 90-day classic PAT pattern.
5. Since release-please dispatches `release.yml` with `GITHUB_TOKEN`, the approval wait in `release-signing` just pauses that run; no change to release-please needed. Verified for the dispatch part; the waiting behaviour is how environments work (Inferred).

**Package identifier.** Documented convention: `Publisher.Package`, Pascal-cased fields, and "Avoid creating multiple publisher folders" ([winget manifest docs][w-manifest]). rigstats uses publisher folder `Codeby`, so `Codeby.ProtonMailMCP` keeps one folder. (Inferred: confirm the exact package name before the first submission; the first version needs `wingetcreate new`, later ones `update`.)

### G2. Homebrew

- **Documented.** homebrew-core requires building from source; prebuilt binaries belong in a **tap** ([Homebrew discussion #4717][h-4717]). GoReleaser deprecated `brews` (formulae) in **v2.10** in favour of `homebrew_casks` ([GoReleaser formulas][h-grf]); its cask docs say casks "are supposed to be signed, even if they are coming from a tap", and offer an `xattr -dr com.apple.quarantine` post-install hook with a warning that it "circumvents macOS security protections" ([GoReleaser casks][h-grc]).
- **Inferred / unconfirmed.** Whether Homebrew sets `com.apple.quarantine` on files of a **formula** (vs a cask) is reported inconsistently. Casks are quarantined by default. Test: install from the tap, run `xattr -l $(which proton-mail-mcp)`.
- **Option without GoReleaser:** a hand-written formula in `dvalfrid/homebrew-tap` with `on_macos do on_arm … url/sha256 … end end` and `bin.install "proton-mail-mcp"`; the release workflow bumps url/sha256 (Formula API: [docs.brew.sh][h-api]).

### G3. Linux

- Options: tarball (today); `.deb`/`.rpm` via **nfpm** (standalone or inside GoReleaser); AUR `-bin` PKGBUILD pointing at the release tarball. No signing infrastructure is needed for the tarball beyond `SHA256SUMS` + the provenance attestation already produced. Inferred.
- Secret storage on Linux needs a Secret Service (see D1); document `pass`-less headless fallback.

### G4. macOS notarisation

- **Inferred (practitioner reports, Apple page could not be fetched).**
  - Gatekeeper checks only **quarantined** files. A browser-downloaded tarball → quarantined binary → an unsigned or ad-hoc-signed binary is blocked ("damaged"/"cannot be opened").
  - Files fetched by `curl`, Go tooling, or (reportedly) Homebrew formulae are not quarantined.
  - A bare Mach-O can be notarised (submit in a zip with `xcrun notarytool submit --wait`) but **cannot be stapled**, so the first run of a quarantined copy checks Apple online.
  - Requirements: Developer ID Application certificate (Apple Developer Program, paid yearly), hardened runtime (`codesign -o runtime --timestamp`), notarisation.
  - Cross-signing from Linux CI is possible with `rcodesign`/`quill` (GoReleaser's notarize uses quill). Unconfirmed in detail.
- **Claude Desktop launching the binary (Inferred / unconfirmed).** Claude Desktop spawns the command from the config. If the binary is quarantined and un-notarised, the launch fails in the background with no visible Gatekeeper dialog, only an error in `mcp-server-*.log`. Test on a Mac.

---

## Recommendations

### 1. `setup`

| Step | Design | Security trade-off |
|---|---|---|
| Detect | A2 order: probe `127.0.0.1:1143/1025` (ask for other ports) → greeting/ID says `Proton Mail Bridge` → else look for an install (registry / `.app` / `/usr/lib/protonmail/bridge` / flatpak) → clear message. | Greeting is forgeable; used for messages only. |
| Certificate | Do **not** ask the user to export. Connect, STARTTLS, take the leaf cert, check subject `O=Proton AG, OU=Proton Mail, CN=127.0.0.1`, self-signed, IP SAN 127.0.0.1. Show the **SHA-256 fingerprint** and ask "Is Bridge running in your tray? Pin this certificate? [y/N]". Store the PEM in our config dir. Windows only: also show "port owned by bridge.exe signed by Proton AG" (advisory). | TOFU: a squatter at setup time could be pinned; mitigated by Bridge-running prompt, subject check, and the Windows owner check. Avoids the exported private key on disk. Much less code than a three-OS owner check. |
| Credentials | Ask username (echoed) and Bridge password (hidden, `golang.org/x/term`). Test with IMAP `LOGIN` over the pinned TLS, then `LOGOUT`. Never log either. | Hidden input keeps it out of scrollback; test login proves the pin + password together. |
| Store | Password → OS credential store (service `proton-mail-mcp`, account = username). Non-secrets → `os.UserConfigDir()/proton-mail-mcp/config.json` (`0600`, dir `0700`): host, ports, username, pinned cert PEM + fingerprint. `.env` stays supported for dev. Flag `--password-store=file` for headless Linux (0600 file, with a warning). | Keychain beats a file on Windows/Linux-desktop; on macOS go-keyring is effectively same-user-readable (D1), no worse than 0600. |
| Register | Offer: (a) Claude Code: run `claude mcp add --scope user proton-mail -- <abs path>`; (b) Claude Desktop: merge into the config file(s) that exist (both normal and MSIX paths on Windows), with backup + atomic write; never put the password in `env`. Print the JSON for manual use. | No secret in client config. Editing another app's config is the riskiest step: backup + merge only. |

### 2. `check`

Read-only, exit code non-zero on failure, one line per check with a fix hint:
config present → password in store → TCP to 127.0.0.1:port (refused = "Bridge not running or uses another port") → greeting is Bridge → STARTTLS → **fingerprint equals pin** (else "certificate changed: factory reset or reinstall? rerun setup") → LOGIN ok → SMTP banner + STARTTLS + same cert → (Windows) port owner/signer advisory → Claude registrations found. The server at startup runs the same checks and logs one clear slog error. No message content is touched.

### 3. Packaging

| Channel | Design | Trade-off |
|---|---|---|
| Windows | Signed exe (Artifact Signing, reuse company profile, **own app registration + OIDC**, `release-signing` approval) in zip; winget **portable** `Codeby.ProtonMailMCP`; no installer. | Approval gate per release; no long-lived secret except the winget PAT. |
| macOS | Phase 1: Homebrew tap formula (unsigned) + tarball with documented caveat. Phase 2 (if a Developer ID is bought): sign + notarise in CI; universal binary. | Unsigned binaries depend on non-quarantining install paths; notarisation costs a yearly fee. |
| Linux | Tarball + `.deb`/`.rpm` via nfpm; AUR `-bin` later. | Low cost; integrity via checksums + attestation. |
| MCPB | Later, after setup/check: one bundle with per-OS binaries, `user_config` (username, ports, `sensitive` password) passed as env. Must still pin the cert: the bundle can run `setup`-style TOFU on first start, or take the fingerprint as a `user_config` field. | One-click install and OS-keychain storage by Claude Desktop, but the password ends up in env (and in the settings panel per mcpb#244), and the MSIX-path problem goes away. |

### New dependencies (each needs owner approval)

| Module | Why | Alternative if refused |
|---|---|---|
| `golang.org/x/term` | Hidden password input (`term.ReadPassword`) on all three OSes; Go team; depends only on `x/sys` (already indirect). | Hand-written console-mode code per OS (Windows `SetConsoleMode`, termios on Unix): more risk. |
| `github.com/zalando/go-keyring` (+ `danieljoos/wincred`, `godbus/dbus/v5`) | The only small, maintained, cgo-free cross-platform keyring; MIT; no advisories. | 0600 file only; or own code (Windows via x/sys, Linux via godbus anyway). Known macOS ACL weakness (D1) should be stated in SECURITY.md. |
| `golang.org/x/sys` (direct, already indirect) | Windows port-owner/Authenticode advisory and Credential Manager calls. | Drop the Windows owner check. |

No new dependency for IMAP/TLS, config JSON or registry reading (`x/sys/windows/registry` is part of x/sys).

---

## What could not be confirmed

- macOS bundle ID of Bridge (`com.protonmail.bridge` vs `ch.protonmail.bridge`) and Proton AG's Apple Team ID.
- Whether the Windows MSI product code is stable across versions; whether uninstallers delete `protonmail/bridge-v3` (and so the cert).
- Proton .deb/.rpm file layout (only the Arch layout was seen).
- Which `claude_desktop_config.json` older Windows MSIX builds read (5.1231 reads the `Claude-Data` one, see §E).
- Whether Claude Desktop verifies `.mcpb` signatures; quarantine on files extracted from a `.mcpb`.
- Homebrew formula quarantine; Gatekeeper behaviour when Claude Desktop spawns a quarantined binary; Apple's stapling wording (page not fetchable).
- Exact winget portable install dirs and multi-arch `wingetcreate update` syntax.
- go-keyring on headless Linux (expected: D-Bus error).
- That the GUI deletes `grpcServerConfig.json` after reading it.

---

## Sources

Source code (commit-pinned):

[b-make]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/Makefile#L17-L94
[b-launcher]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/cmd/launcher/main.go#L51
[b-versioner]: https://github.com/ProtonMail/proton-bridge/tree/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/versioner
[b-locations]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/locations/locations.go#L83
[b-provider]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/locations/provider.go#L41
[b-app]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/app/app.go#L58
[b-const]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/constants/constants.go#L28
[b-listener]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapsmtpserver/listener.go#L28
[b-single]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/app/singleinstance.go#L37
[b-migration]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/app/migration.go#L110
[b-typescerts]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/types_certs.go
[b-typesdata]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/types_data.go#L32
[b-vault]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/vault.go#L64
[b-vaulthelper]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/helper.go#L29
[b-appvault]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/app/vault.go#L78
[b-certs]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/certs/tls.go#L40
[b-vaultcerts]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/certs.go
[b-svccert]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/frontend/grpc/service_cert.go#L63
[b-clisystem]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/frontend/cli/system.go#L288
[b-certdarwin]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/certs/cert_store_darwin.go
[b-certwin]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/certs/cert_store_windows.go
[b-bridge]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/bridge/bridge.go#L652
[b-settings]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/bridge/settings.go#L320
[b-settings-types]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/vault/types_settings.go#L76
[b-grpc]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/frontend/grpc/service.go#L119
[b-svcconfig]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/service/config.go#L27
[b-imap]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapsmtpserver/imap.go#L193
[b-smtp]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/internal/services/imapsmtpserver/smtp.go#L49
[b-desktop]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/dist/proton-bridge.desktop
[b-gomod]: https://github.com/ProtonMail/proton-bridge/blob/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/go.mod
[b-keychain]: https://github.com/ProtonMail/proton-bridge/tree/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2/pkg/keychain
[g-greet]: https://github.com/ProtonMail/gluon/blob/7e800978ab4a9942829eb6116e33ea0278c03e91/internal/session/session.go#L345
[s-greet]: https://github.com/ProtonMail/go-smtp/blob/2b3d50599865839dfccb6bf4805996fc3d476f29/conn.go#L926
[k-win]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/keyring_windows.go#L30
[k-darwin]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/keyring_darwin.go#L70
[k-unix]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/keyring_unix.go
[k-ss]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/secret_service/secret_service.go#L73
[k-gomod]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/go.mod
[k-keyring]: https://github.com/zalando/go-keyring/blob/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a/keyring.go#L16
[x-zsys]: https://github.com/golang/sys/blob/6f4195bbcd27fffa838e6221a66c401d21944089/windows/zsyscall_windows.go
[x-types]: https://github.com/golang/sys/blob/6f4195bbcd27fffa838e6221a66c401d21944089/windows/types_windows.go#L1704
[x-darwin]: https://github.com/golang/sys/blob/6f4195bbcd27fffa838e6221a66c401d21944089/unix/syscall_darwin.go#L503
[m-manifest]: https://github.com/modelcontextprotocol/mcpb/blob/70fe3b34cd6dff1b3bba046638edc72a6467a4fb/MANIFEST.md
[m-schema]: https://github.com/modelcontextprotocol/mcpb/blob/70fe3b34cd6dff1b3bba046638edc72a6467a4fb/schemas/mcpb-manifest-latest.schema.json
[m-cli]: https://github.com/modelcontextprotocol/mcpb/blob/70fe3b34cd6dff1b3bba046638edc72a6467a4fb/CLI.md
[r-release]: https://github.com/dvalfrid/rigstats/blob/918900938ce11c1967cf5093228d9249a951d0dd/.github/workflows/release.yml
[r-winget]: https://github.com/dvalfrid/rigstats/blob/918900938ce11c1967cf5093228d9249a951d0dd/.github/workflows/winget-submit.yml
[r-docs]: https://github.com/dvalfrid/rigstats/blob/918900938ce11c1967cf5093228d9249a951d0dd/docs/release.md
[w-bridge]: https://github.com/microsoft/winget-pkgs/blob/0d87e8df4055d9b29fe749d8a60559d0c901333f/manifests/p/Proton/ProtonMailBridge/3.27.1/Proton.ProtonMailBridge.installer.yaml

Documentation and reports:

[d-cli]: https://proton.me/support/bridge-cli-guide
[d-verify]: https://proton.me/support/verifying-bridge-package
[d-cask]: https://formulae.brew.sh/cask/proton-mail-bridge
[d-arch]: https://archlinux.org/packages/extra/x86_64/protonmail-bridge-core/files/
[d-flathub]: https://flathub.org/en/apps/ch.protonmail.protonmail-bridge
[d-mcplocal]: https://modelcontextprotocol.io/docs/develop/connect-local-servers
[d-ccmcp]: https://code.claude.com/docs/en/mcp
[d-support]: https://support.claude.com/en/articles/10949351-getting-started-with-local-mcp-servers-on-claude-desktop
[d-dxt]: https://www.anthropic.com/research/desktop-extensions
[e-25579]: https://github.com/anthropics/claude-code/issues/25579
[e-26073]: https://github.com/anthropics/claude-code/issues/26073
[m-244]: https://github.com/modelcontextprotocol/mcpb/issues/244
[k-acl]: https://github.com/branow/atlassian-cli/issues/20
[k-158]: https://github.com/zalando/go-keyring/pull/158
[k-153]: https://github.com/zalando/go-keyring/issues/153
[w-manifest]: https://learn.microsoft.com/en-us/windows/package-manager/package/manifest
[w-schema]: https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.12.0/installer.md
[w-6604]: https://github.com/microsoft/winget-cli/issues/6604
[w-2498]: https://github.com/microsoft/winget-cli/issues/2498
[w-beads]: https://github.com/gastownhall/beads/issues/4908
[a-oidc]: https://github.com/Azure/artifact-signing-action/blob/main/docs/OIDC.md
[h-4717]: https://github.com/orgs/Homebrew/discussions/4717
[h-grf]: https://goreleaser.com/customization/homebrew_formulas/
[h-grc]: https://goreleaser.com/customization/homebrew_casks/
[h-api]: https://docs.brew.sh/rubydoc/Formula

- Bridge source: <https://github.com/ProtonMail/proton-bridge/tree/36d7b6a63256bbf4cf0b3fba9bb7854db32abff2>
- gluon: <https://github.com/ProtonMail/gluon/tree/7e800978ab4a9942829eb6116e33ea0278c03e91>
- go-keyring: <https://github.com/zalando/go-keyring/tree/66b55cc0c51cd3d8dabc0ac70da30f058b2fe54a>
- MCPB: <https://github.com/modelcontextprotocol/mcpb/tree/70fe3b34cd6dff1b3bba046638edc72a6467a4fb>
- x/sys: <https://github.com/golang/sys/tree/6f4195bbcd27fffa838e6221a66c401d21944089>
- rigstats: <https://github.com/dvalfrid/rigstats/tree/918900938ce11c1967cf5093228d9249a951d0dd>
- winget manifest docs: <https://learn.microsoft.com/en-us/windows/package-manager/package/manifest>
- winget installer schema 1.12.0: <https://github.com/microsoft/winget-pkgs/blob/master/doc/manifest/schema/1.12.0/installer.md>
- Azure artifact-signing-action OIDC: <https://github.com/Azure/artifact-signing-action/blob/main/docs/OIDC.md>
- MCP local servers: <https://modelcontextprotocol.io/docs/develop/connect-local-servers>
- Claude Code MCP: <https://code.claude.com/docs/en/mcp>
- Anthropic Desktop Extensions: <https://www.anthropic.com/research/desktop-extensions>
- Claude support, local MCP servers: <https://support.claude.com/en/articles/10949351-getting-started-with-local-mcp-servers-on-claude-desktop>
- Proton Bridge CLI guide: <https://proton.me/support/bridge-cli-guide>
- Flathub Bridge: <https://flathub.org/en/apps/ch.protonmail.protonmail-bridge>
- Arch package files: <https://archlinux.org/packages/extra/x86_64/protonmail-bridge-core/files/>
- GoReleaser Homebrew formulas/casks: <https://goreleaser.com/customization/homebrew_formulas/>, <https://goreleaser.com/customization/homebrew_casks/>
- go-keyring macOS ACL report: <https://github.com/branow/atlassian-cli/issues/20>; PR #158, issue #153 (links above)
- Claude Desktop MSIX config issues: anthropics/claude-code #25579, #26073 (links above)
