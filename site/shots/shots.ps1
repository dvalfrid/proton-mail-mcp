#!/usr/bin/env pwsh
# Captures the setup wizard's five steps, light and dark, into site/img, from
# site/shots/main.go (the real page with invented data). Windows only. Run
# from the repository root after changing the wizard's page:
#
#   ./site/shots/shots.ps1
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System; using System.Runtime.InteropServices;
public static class Shot {
  [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindowW(string c, string t);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  [DllImport("user32.dll")] public static extern bool GetClientRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr hdc, uint f);
  [DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(IntPtr v);
}
"@
[Shot]::SetProcessDpiAwarenessContext([IntPtr](-4)) | Out-Null

$exe = Join-Path ([IO.Path]::GetTempPath()) 'pmcp-shots.exe'
& go build -o $exe site/shots/main.go
if ($LASTEXITCODE -ne 0) { throw 'build failed' }
New-Item -ItemType Directory -Force site/img | Out-Null
$names = 'welcome', 'bridge', 'account', 'claude', 'done'
try {
    foreach ($mode in 'light', 'dark') {
        foreach ($step in 0..4) {
            $p = Start-Process $exe -ArgumentList $step, $mode -PassThru
            $sw = [Diagnostics.Stopwatch]::StartNew(); $h = [IntPtr]::Zero
            while ($sw.ElapsedMilliseconds -lt 10000) {
                $h = [Shot]::FindWindowW([NullString]::Value, 'pmcp-shot')
                if ($h -ne [IntPtr]::Zero -and [Shot]::IsWindowVisible($h)) { break }
                Start-Sleep -Milliseconds 50
            }
            if ($h -eq [IntPtr]::Zero) { throw "no window for step $step" }
            Start-Sleep -Milliseconds 2500 # load, the step, its animation
            $r = New-Object Shot+RECT; [Shot]::GetClientRect($h, [ref]$r) | Out-Null
            $bmp = New-Object System.Drawing.Bitmap $r.R, $r.B
            $g = [System.Drawing.Graphics]::FromImage($bmp); $hdc = $g.GetHdc()
            [Shot]::PrintWindow($h, $hdc, 3) | Out-Null # PW_CLIENTONLY | PW_RENDERFULLCONTENT
            $g.ReleaseHdc($hdc); $g.Dispose()
            $out = "site/img/wizard-$($names[$step])-$mode.png"
            $bmp.Save((Join-Path $PWD $out), [System.Drawing.Imaging.ImageFormat]::Png); $bmp.Dispose()
            Stop-Process -Id $p.Id
            "$out ($($r.R)x$($r.B))"
        }
    }
} finally {
    Remove-Item $exe -ErrorAction SilentlyContinue
}
