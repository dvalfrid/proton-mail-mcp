# Assets

The project icon: an envelope with a shield and check.

| File | Use |
|---|---|
| `icon.svg` | The source. Edit this one |
| `png/icon-<size>.png` | 16 to 512 px, transparent background: web pages, README, social preview |
| `icon.ico` | Windows (16 to 256 px): built into the exe files by `./tools/task.ps1 build` (go-winres), and for the installer, shortcuts, uninstall entry |

The PNGs and `icon.ico`, and the copies in `internal/wizard/icons` (the setup wizard's window icon), are drawn from `icon.svg`. After changing it, on Windows, run from the repository root:

```powershell
go run assets/genicons.go
```
