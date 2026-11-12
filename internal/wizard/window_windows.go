package wizard

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/jchv/go-webview2/pkg/edge"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// The project icon (assets/icon.svg) in the sizes Windows asks for, drawn by
// assets/genicons.go.
//
//go:embed icons/*.png
var icons embed.FS

var iconSizes = []int{16, 20, 24, 32, 40, 48, 64}

// The window's client size in pixels at 100% scaling.
const width, height = 780, 560

// The page's background in each mode (page.html, .win.light and .win.dark
// --bg), so the window never shows white before the page is drawn.
var background = map[bool][3]uint8{false: {0xf3, 0xf3, 0xf3}, true: {0x20, 0x20, 0x20}}

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procShowWindow               = user32.NewProc("ShowWindow")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW       = user32.NewProc("PostThreadMessageW")
	procAdjustWindowRect         = user32.NewProc("AdjustWindowRect")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procLoadCursorW              = user32.NewProc("LoadCursorW")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procSetTimer                 = user32.NewProc("SetTimer")
	procKillTimer                = user32.NewProc("KillTimer")
	procSetProcessDpiAwarenessC  = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForSystem          = user32.NewProc("GetDpiForSystem")
	procGetSystemMetricsForDpi   = user32.NewProc("GetSystemMetricsForDpi")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	gdi32                        = windows.NewLazySystemDLL("gdi32.dll")
	procCreateSolidBrush         = gdi32.NewProc("CreateSolidBrush")
	dwmapi                       = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttribute    = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wmDestroy  = 0x0002
	wmMove     = 0x0003
	wmSize     = 0x0005
	wmActivate = 0x0006
	wmClose    = 0x0010
	wmTimer    = 0x0113
	wmApp      = 0x8000

	swHide = 0
	swShow = 5

	// WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_MINIMIZEBOX: a fixed-size
	// dialog-like window that can be minimised.
	windowStyle = 0x00C00000 | 0x00080000 | 0x00020000

	showTimer = 1
)

type wndClassEx struct {
	size       uint32
	style      uint32
	wndProc    uintptr
	clsExtra   int32
	wndExtra   int32
	instance   uintptr
	icon       uintptr
	cursor     uintptr
	background uintptr
	menuName   *uint16
	className  *uint16
	iconSm     uintptr
}

type winMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       [2]int32
	lPrivate uint32
}

type winRect struct{ left, top, right, bottom int32 }

// window is the wizard's one window: a plain Win32 window that WebView2
// fills. It is created hidden and shown only when the page has drawn itself,
// and hidden again before it is torn down, so it never flashes white.
type window struct {
	hwnd   uintptr
	thread uint32
	wv     *edge.Chromium
	shown  bool

	mu    sync.Mutex
	queue []func()
}

// current is the window, for wndProc. There is only ever one.
var current *window

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
	dataDir, err := DataPath()
	if err != nil {
		return err
	}
	isDark := dark()
	bg := background[isDark]

	w := &window{thread: windows.GetCurrentThreadId()}
	current = w
	if err := w.create(title, scale, bg); err != nil {
		return err
	}
	if isDark {
		on := int32(1)
		// DWMWA_USE_IMMERSIVE_DARK_MODE (20): a dark title bar, as Windows'
		// own apps have in dark mode. Best effort.
		_, _, _ = procDwmSetWindowAttribute.Call(w.hwnd, 20, uintptr(unsafe.Pointer(&on)), 4) //nolint:gosec // G103: Win32 call with a pointer to a local int32 of the size passed
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.wv = edge.NewChromium()
	w.wv.DataPath = dataDir
	w.wv.MessageCallback = func(m string) { w.onMessage(ctx, b, m) }
	if !w.wv.Embed(w.hwnd) {
		_, _, _ = procDestroyWindow.Call(w.hwnd)
		return ErrNoWebView
	}
	w.wv.Resize()
	if err := w.configure(bg); err != nil {
		_, _, _ = procDestroyWindow.Call(w.hwnd)
		return err
	}
	b.SetQuit(func() { w.dispatch(w.close) })

	// The page calls the program through __pmcp and says when it has drawn
	// itself through __pmcpReady.
	w.wv.Init(`window.__pmcp = (id, method, args) => { window.chrome.webview.postMessage(JSON.stringify({id, method, args})); return Promise.resolve(); };
window.__pmcpReady = () => window.chrome.webview.postMessage(JSON.stringify({id: 0, method: "ready"}));`)
	w.wv.NavigateToString(Page)
	// Show the window after a while even if the page never says it is ready,
	// so a broken page can at least be closed.
	_, _, _ = procSetTimer.Call(w.hwnd, showTimer, 4000, 0)
	w.loop()
	return nil
}

func (w *window) create(title string, scale float64, bg [3]uint8) error {
	var instance windows.Handle
	if err := windows.GetModuleHandleEx(0, nil, &instance); err != nil {
		return err
	}
	className := windows.StringToUTF16Ptr("ProtonMailMCPSetup")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)           // IDC_ARROW
	icon, iconSm := loadIcon(11, scale), loadIcon(49, scale) // SM_CXICON, SM_CXSMICON
	if icon == 0 || iconSm == 0 {
		icon, _, _ = procLoadIconW.Call(0, 32512) // IDI_APPLICATION
		iconSm = icon
	}
	brush, _, _ := procCreateSolidBrush.Call(uintptr(bg[0]) | uintptr(bg[1])<<8 | uintptr(bg[2])<<16)
	wc := wndClassEx{
		wndProc: windows.NewCallback(wndProc), instance: uintptr(instance), icon: icon, iconSm: iconSm,
		cursor: cursor, background: brush, className: className,
	}
	wc.size = uint32(unsafe.Sizeof(wc))
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 { //nolint:gosec // G103: Win32 call with a pointer to the struct it reads
		return fmt.Errorf("register the window class: %w", err)
	}

	r := winRect{right: int32(float64(width) * scale), bottom: int32(float64(height) * scale)}
	_, _, _ = procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), windowStyle, 0) //nolint:gosec // G103: Win32 call that adjusts the local rect
	ww, wh := r.right-r.left, r.bottom-r.top
	sw, _, _ := procGetSystemMetrics.Call(0)                   // SM_CXSCREEN
	sh, _, _ := procGetSystemMetrics.Call(1)                   // SM_CYSCREEN
	x, y := max((int32(sw)-ww)/2, 0), max((int32(sh)-wh)/2, 0) //nolint:gosec // G115: screen sizes in pixels fit an int32

	// Not WS_VISIBLE: shown once the page has drawn itself.
	hwnd, _, err := procCreateWindowExW.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(windows.StringToUTF16Ptr(title))), //nolint:gosec // G103: Win32 call with NUL-terminated UTF-16 strings
		windowStyle, uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, uintptr(instance), 0) //nolint:gosec // G115: x and y are at least 0, and the size is positive
	if hwnd == 0 {
		return fmt.Errorf("create the window: %w", err)
	}
	w.hwnd = hwnd
	return nil
}

// loadIcon makes the project icon at the size Windows uses for metric (a
// GetSystemMetrics index), from the smallest embedded PNG at least that
// big. 0 if it can't.
func loadIcon(metric uintptr, scale float64) uintptr {
	size := 0
	if procGetSystemMetricsForDpi.Find() == nil {
		v, _, _ := procGetSystemMetricsForDpi.Call(metric, uintptr(96*scale))
		size = int(v)
	}
	if size <= 0 {
		v, _, _ := procGetSystemMetrics.Call(metric)
		size = int(float64(v) * scale)
	}
	best := iconSizes[len(iconSizes)-1]
	for _, s := range iconSizes {
		if s >= size {
			best = s
			break
		}
	}
	data, err := icons.ReadFile(fmt.Sprintf("icons/icon-%d.png", best))
	if err != nil || len(data) == 0 {
		return 0
	}
	// fIcon TRUE, version 0x00030000; PNG data is accepted since Vista.
	h, _, _ := procCreateIconFromResourceEx.Call(uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), 1, 0x00030000, uintptr(size), uintptr(size), 0) //nolint:gosec // G103: Win32 call reading the embedded PNG bytes
	return h
}

// configure locks the web view down to a single page: no developer tools,
// context menu, browser shortcuts (reload, back, print, find), zoom or
// swipe navigation, and the page's background behind it.
func (w *window) configure(bg [3]uint8) error {
	s, err := w.wv.GetSettings()
	if err != nil {
		return fmt.Errorf("WebView2 settings: %w", err)
	}
	for _, f := range []func(bool) error{
		s.PutAreDevToolsEnabled, s.PutAreDefaultContextMenusEnabled, s.PutAreBrowserAcceleratorKeysEnabled,
		s.PutIsZoomControlEnabled, s.PutIsPinchZoomEnabled, s.PutIsSwipeNavigationEnabled,
		s.PutIsStatusBarEnabled, s.PutAreDefaultScriptDialogsEnabled, s.PutAreHostObjectsAllowed,
	} {
		if err := f(false); err != nil {
			return fmt.Errorf("WebView2 settings: %w", err)
		}
	}
	if c2 := w.wv.GetController().GetICoreWebView2Controller2(); c2 != nil {
		_ = c2.PutDefaultBackgroundColor(edge.COREWEBVIEW2_COLOR{A: 255, R: bg[0], G: bg[1], B: bg[2]}) // cosmetic: the window's brush is the same colour
	}
	return nil
}

// loop runs the window's messages until it is destroyed.
func (w *window) loop() {
	var m winMsg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0) //nolint:gosec // G103: Win32 call that fills the local message
		if int32(r) <= 0 {                                                    //nolint:gosec // G115: GetMessage returns a BOOL (0 for WM_QUIT, -1 for an error) in the low 32 bits
			return
		}
		if m.hwnd == 0 && m.message == wmApp {
			w.runQueue()
			continue
		}
		_, _, _ = procTranslateMessage.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 call with the message GetMessage filled
		_, _, _ = procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m))) //nolint:gosec // G103: Win32 call with the message GetMessage filled
	}
}

// dispatch runs f on the window's thread.
func (w *window) dispatch(f func()) {
	w.mu.Lock()
	w.queue = append(w.queue, f)
	w.mu.Unlock()
	_, _, _ = procPostThreadMessageW.Call(uintptr(w.thread), wmApp, 0, 0)
}

func (w *window) runQueue() {
	w.mu.Lock()
	q := w.queue
	w.queue = nil
	w.mu.Unlock()
	for _, f := range q {
		f()
	}
}

func (w *window) show() {
	if w.shown {
		return
	}
	w.shown = true
	_, _, _ = procKillTimer.Call(w.hwnd, showTimer)
	_, _, _ = procShowWindow.Call(w.hwnd, swShow)
	// WebView2 doesn't follow its parent window: made while the window was
	// hidden, it must be told to show.
	_ = w.wv.Show() // if it fails, the window shows the page's background colour, never white
	w.wv.Resize()
	_, _, _ = procSetForegroundWindow.Call(w.hwnd)
	w.wv.Focus()
}

// close hides the window before tearing it down, so nothing white shows.
func (w *window) close() {
	_, _, _ = procShowWindow.Call(w.hwnd, swHide)
	_, _, _ = procDestroyWindow.Call(w.hwnd)
}

// onMessage handles one message from the page, on the window's thread.
// Calls run off it, so a slow login doesn't freeze the window; the answer
// goes back through dispatch.
func (w *window) onMessage(ctx context.Context, b *Backend, s string) {
	var m struct {
		ID     int             `json:"id"`
		Method string          `json:"method"`
		Args   json.RawMessage `json:"args"`
	}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return // not from the page
	}
	if m.Method == "ready" {
		w.show()
		return
	}
	go func() {
		res, err := call(ctx, b, m.Method, m.Args)
		payload, ok := marshalReply(res, err)
		w.dispatch(func() { w.wv.Eval(fmt.Sprintf("window.__pmcpDone(%d,%t,%s)", m.ID, ok, payload)) })
	}()
}

func wndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	w := current
	switch msg {
	case wmSize:
		if w != nil && w.wv != nil {
			w.wv.Resize()
		}
	case wmMove:
		if w != nil && w.wv != nil {
			_ = w.wv.NotifyParentWindowPositionChanged() // only moves popups along
		}
	case wmActivate:
		if w != nil && w.wv != nil && wParam&0xffff != 0 {
			w.wv.Focus()
		}
	case wmTimer:
		if w != nil && w.wv != nil {
			w.show()
		}
		return 0
	case wmClose:
		_, _, _ = procShowWindow.Call(hwnd, swHide)
		_, _, _ = procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		_, _, _ = procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
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
