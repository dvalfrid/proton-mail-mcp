package wizard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// The window's client size in pixels at 100% scaling.
const width, height = 780, 560

var (
	user32                      = windows.NewLazySystemDLL("user32.dll")
	procSetProcessDpiAwarenessC = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem         = user32.NewProc("GetDpiForSystem")
	procMessageBoxW             = user32.NewProc("MessageBoxW")
	dwmapi                      = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute   = dwmapi.NewProc("DwmSetWindowAttribute")
)

// Run shows the wizard and returns when its window closes. It must be called
// from the main goroutine. ErrNoWebView means WebView2 isn't available.
func Run(b *Backend, title string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Per-monitor DPI awareness (-4), so the page is sharp at 125% or 150%
	// instead of stretched. Best effort: older Windows lacks the call.
	if procSetProcessDpiAwarenessC.Find() == nil {
		_, _, _ = procSetProcessDpiAwarenessC.Call(^uintptr(3))
	}
	scale := 1.0
	if procGetDpiForSystem.Find() == nil {
		if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
			scale = float64(dpi) / 96
		}
	}

	dataDir, err := dataPath()
	if err != nil {
		return err
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath: dataDir,
		WindowOptions: webview2.WindowOptions{
			Title: title, Width: uint(float64(width) * scale), Height: uint(float64(height) * scale), Center: true,
		},
	})
	if w == nil {
		return ErrNoWebView
	}
	defer w.Destroy()
	w.SetSize(int(float64(width)*scale), int(float64(height)*scale), webview2.HintFixed)
	if dark() {
		on := int32(1)
		// DWMWA_USE_IMMERSIVE_DARK_MODE (20): a dark title bar, as Windows'
		// own apps have in dark mode. Best effort.
		_, _, _ = procDwmSetWindowAttribute.Call(uintptr(w.Window()), 20, uintptr(unsafe.Pointer(&on)), 4) //nolint:gosec // G103: Win32 call with a pointer to a local int32 of the size passed
	}

	b.SetQuit(func() { w.Dispatch(w.Terminate) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Calls run off the UI thread, so a slow login doesn't freeze the
	// window; the answer goes back through Dispatch.
	err = w.Bind("__pmcp", func(id int, method string, args json.RawMessage) {
		go func() {
			res, err := call(ctx, b, method, args)
			payload, ok := marshalReply(res, err)
			w.Dispatch(func() { w.Eval(fmt.Sprintf("window.__pmcpDone(%d,%t,%s)", id, ok, payload)) })
		}()
	})
	if err != nil {
		return err
	}
	w.SetHtml(Page)
	w.Run()
	return nil
}

// call runs one call and turns a panic into an error, so the page gets an
// answer and the window stays up.
func call(ctx context.Context, b *Backend, method string, args json.RawMessage) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			mail.LogPanic(b.o.Logger, "wizard "+method, r)
			res, err = nil, errors.New("internal error")
		}
	}()
	return b.Call(ctx, method, args)
}

// marshalReply is the JSON sent to the page: the result, or the error text.
func marshalReply(res any, err error) (string, bool) {
	if err != nil {
		data, _ := json.Marshal(err.Error()) // a string always marshals
		return string(data), false
	}
	data, merr := json.Marshal(res)
	if merr != nil {
		data, _ = json.Marshal(merr.Error()) // a string always marshals
		return string(data), false
	}
	return string(data), true
}

// dataPath is WebView2's profile folder, under the user's local app data
// (by default it would go in the roaming profile).
func dataPath() (string, error) {
	dir, err := os.UserCacheDir() // %LOCALAPPDATA%
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "proton-mail-mcp", "WebView2"), nil
}

// dark reports whether Windows apps are set to dark mode.
func dark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer func() { _ = k.Close() }() // read-only key
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

// SystemAccent reads the Windows accent palette: Windows 11 controls use
// its first darker shade in light mode and its second lighter shade in dark
// mode. Empty when it can't be read; the page then uses Windows' default.
func SystemAccent() Accent {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\Accent`, registry.QUERY_VALUE)
	if err != nil {
		return Accent{}
	}
	defer func() { _ = k.Close() }() // read-only key
	p, _, err := k.GetBinaryValue("AccentPalette")
	if err != nil || len(p) < 32 {
		return Accent{}
	}
	// Eight RGBA entries: light 3, light 2, light 1, accent, dark 1, dark 2,
	// dark 3, and one more.
	rgb := func(i int) string { return fmt.Sprintf("#%02x%02x%02x", p[i*4], p[i*4+1], p[i*4+2]) }
	return Accent{Light: rgb(4), Dark: rgb(1)}
}

// OpenURL opens url in the user's browser.
func OpenURL(url string) error {
	return windows.ShellExecute(0, windows.StringToUTF16Ptr("open"), windows.StringToUTF16Ptr(url), nil, nil, windows.SW_SHOWNORMAL)
}

// Alert shows a message box: the only way a windowless program can report
// a problem.
func Alert(title, text string, warning bool) {
	flags := uintptr(0x40) // MB_ICONINFORMATION
	if warning {
		flags = 0x30 // MB_ICONWARNING
	}
	t, _ := windows.UTF16PtrFromString(title) // no NUL in our own texts
	m, _ := windows.UTF16PtrFromString(text)
	_, _, _ = procMessageBoxW.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), flags) //nolint:gosec // G103: Win32 call with two NUL-terminated UTF-16 strings
}

// TerminalSetup starts `<exe> setup` in a new console window that stays open
// afterwards: the fallback when WebView2 is missing.
func TerminalSetup(exe string) error {
	// Not CommandContext: the console outlives this program on purpose.
	cmd := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")) //nolint:gosec,noctx // G204: Windows' own cmd.exe and this program's sibling exe; noctx: started, never waited for
	cmd.SysProcAttr = &syscall.SysProcAttr{
		// Windows paths can't contain quotes, so quoting the path is enough.
		CmdLine:       `cmd.exe /k ""` + exe + `" setup"`,
		CreationFlags: windows.CREATE_NEW_CONSOLE,
	}
	return cmd.Start()
}
