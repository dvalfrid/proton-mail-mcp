---
name: pre-release
description: Publish a release candidate (GitHub pre-release with the signed installers) to test the installer before a real release, without it becoming Latest or reaching winget — pick the code and the version, create the pre-release, dispatch the build, guide the owner through the signing approval, check the result, hand over the link, clean up. Use when the owner says "gör en RC", "pre-release", "release candidate", or wants to try the installer of main or an unmerged PR.
---

What a pre-release reaches, and what it doesn't — checked against the code:

- **Built and signed like a release.** `release.yml` builds whatever tag it is given (it checks out `inputs.tag`), signs the Windows exes and installers after the owner approves `release-signing`, then uploads to that tag's release.
- **Not Latest.** The website's download buttons go to `releases/latest`, which never points at a pre-release. **Latest** stays on the last real release.
- **Not sent to winget.** Submission is by hand today. If it is ever automated, it must skip pre-releases.
- **Visible to anyone** on the releases page, marked **Pre-release**.
- **The version is the tag's:** `0.4.0-rc.1` in the binaries (`-X main.version`), the installer's title and "Installed apps". The file version is `0.4.0.0`, the same as the real release's. Installing the real release over an RC is an ordinary update.
- **Release Please doesn't move:** its version comes from `.release-please-manifest.json`, not from tags.

## 1. Which code, which version

- **Code:** ask if it isn't said: `main` (what is merged) or a PR/branch (not merged yet). Use the commit **on origin**: `git fetch` then `git rev-parse origin/<branch>`. The build uses `release.yml` from `main` but the code, the installer script and `task.ps1` at the tag.
- **Version:** the next real version + `-rc.N`.
  - Open release PR (`gh pr list --search "chore(main): release" --state open`): its version, e.g. `chore(main): release 0.4.0` → `v0.4.0-rc.1`.
  - No release PR: `.release-please-manifest.json`'s version with the minor +1 (before 1.0 a feature bumps the minor), and tell the owner the real number may differ.
  - Earlier RCs of the same version (`gh release list | grep -- -rc.`): the next N.

## 2. Create it

```bash
gh release create v<X.Y.Z>-rc.<N> --target <sha> --prerelease --latest=false \
  --title "v<X.Y.Z>-rc.<N> (release candidate)" \
  --notes "Release candidate for testing, not the latest release. <what to test>"
```

**Never without `--prerelease`.** A normal release becomes Latest: the website offers it. Check right after: `gh release view <tag> --json isPrerelease` must say `true`. If it says `false`, delete it (`gh release delete <tag> --cleanup-tag --yes`) before building.

Then start the build, which nothing else does for a hand-made release:

```bash
gh workflow run release.yml --ref main -f tag=v<X.Y.Z>-rc.<N>
```

## 3. Approval

- Find the run: `gh run list --workflow release.yml --limit 1`.
- Wait in the background (`gh run watch <id>`, run in the background) until the `sign-windows` job is waiting, then tell the owner: run link → **Review deployments** → tick `release-signing` → **Approve and deploy**.
- If a build job fails, nothing is signed or published. Show the failing step's log (`gh run view <id> --log-failed`).

## 4. Check

- The run is `success`.
- `gh release view <tag> --json assets` lists `proton-mail-mcp_<X.Y.Z>-rc.<N>_windows_amd64_setup.exe` (and arm64), the zips and `SHA256SUMS.txt`.
- `gh release list --limit 3`: **Latest** is still the last real release.
- Hand over the link: `https://github.com/dvalfrid/proton-mail-mcp/releases/tag/<tag>`.

## 5. Clean up

Only when the owner says so (deleting can't be undone): `gh release delete <tag> --cleanup-tag --yes`. A kept RC does no harm: it stays marked Pre-release.
