//go:build ignore

// shots shows the window's real page (internal/wizard/page.html) at one
// setup step or one app page, with invented data (alice@example.com)
// instead of a real backend, in a window titled "pmcp-shot", for
// site/shots/shots.ps1 to capture. Windows only (WebView2). Usage:
//
//	go run site/shots/main.go <step 0-4 | status | settings | about> <light|dark>
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	webview2 "github.com/jchv/go-webview2"

	"github.com/dvalfrid/proton-mail-mcp/internal/wizard"
)

// Twice the window's 780x720, for sharp images on high-density screens.
const zoom, width, height = 2, 780, 720

// mock answers the page's calls with invented data.
const mock = `
window.__pmcp = (id, method, args) => {
  const reply = v => setTimeout(() => window.__pmcpDone(id, true, v), 0);
  switch (method) {
    case 'state': reply(window.__app ? { platform: 'windows', mode: 'app', version: '0.6.0', user: 'alice@example.com', savedPassword: true,
      store: 'Windows Credential Manager', canVerifyOwner: true, accent: {},
      exe: 'C:\\Users\\alice\\AppData\\Local\\Programs\\proton-mail-mcp\\proton-mail-mcp.exe',
      clients: [{ name: 'Claude Desktop', registered: true }, { name: 'Claude Code', registered: true }], tools: [] }
      : { platform: 'windows', version: '0.6.0', user: 'alice@example.com', savedPassword: false,
      store: 'Windows Credential Manager', canVerifyOwner: true, accent: {},
      exe: 'C:\\Program Files\\proton-mail-mcp\\proton-mail-mcp.exe',
      clients: [{ name: 'Claude Desktop', registered: false }, { name: 'Claude Code', registered: false }] }); break;
    case 'runCheck': reply({ failed: false, steps: [
      { name: 'settings', title: 'Settings', status: 'ok', detail: 'config.json and the saved password' },
      { name: 'bridge', title: 'Proton Mail Bridge', status: 'ok', detail: '127.0.0.1:1143 answers' },
      { name: 'cert', title: 'Certificate', status: 'ok', detail: 'matches the pinned one' },
      { name: 'login', title: 'Sign-in', status: 'ok', detail: 'alice@example.com' },
      { name: 'smtp', title: 'Sending', status: 'ok', detail: '127.0.0.1:1025 answers' }] }); break;
    case 'getSettings': case 'saveSettings': reply({ setUp: true, allowRemoteImages: false, attachmentDir: '',
      defaultAttachmentDir: 'C:\\Users\\alice\\Downloads\\Proton Mail MCP', outboxDir: '',
      defaultOutboxDir: 'C:\\Users\\alice\\Documents\\Proton Mail MCP outbox', maxChanges: 200, defaultMaxChanges: 200, allowSend: true, locked: {},
      labelColors: { Work: 'blue', Family: 'green', Receipts: 'yellow' },
      colors: [['red', '🔴'], ['orange', '🟠'], ['yellow', '🟡'], ['green', '🟢'], ['blue', '🔵'], ['purple', '🟣'], ['brown', '🟤'], ['black', '⚫'], ['white', '⚪']].map(([name, emoji]) => ({ name, emoji })) }); break;
    case 'checkUpdate': reply({ running: '0.6.0', latest: '0.6.0', update_available: false, message: 'You have the latest version (0.6.0).' }); break;
    case 'findBridge': reply({ addr: '127.0.0.1:1143', answers: true, isBridge: true, installed: true,
      fingerprint: '5C:1E:9A:04:7D:B2:63:F0:8A:21:CE:47:93:0B:D8:6F:12:A5:3E:C9:70:4D:E1:88:2B:96:F4:0C:57:A3:1D:6E',
      looksLikeBridge: true, ownerKnown: true, ownerPath: 'C:\\Program Files\\Proton AG\\Proton Mail Bridge\\bridge.exe', verified: true }); break;
    default: reply(null);
  }
  return Promise.resolve();
};`

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: go run site/shots/main.go <step 0-4 | status | settings | about> <light|dark>")
	}
	page := os.Args[1]
	step, err := strconv.Atoi(page)
	app := page == "status" || page == "settings" || page == "about"
	if !app && (err != nil || step < 0 || step > 4) {
		log.Fatal("give a step 0 to 4, or status, settings or about")
	}
	mode := os.Args[2]

	w := webview2.NewWithOptions(webview2.WebViewOptions{WindowOptions: webview2.WindowOptions{Title: "pmcp-shot", Center: true}})
	if w == nil {
		log.Fatal("WebView2 is not available")
	}
	defer w.Destroy()
	w.SetSize(width*zoom, height*zoom, webview2.HintFixed)
	if app {
		w.Init("window.__app = true;")
	}
	w.Init(mock)
	// After the page has loaded its state: the mode, the zoom, and the step.
	w.Init(fmt.Sprintf(`addEventListener('load', () => setTimeout(() => {
  document.documentElement.style.zoom = '%d';
  document.querySelector('.window').style.height = '%dpx';
  const mode = () => { document.body.className = 'win %s'; };
  mode(); theme = mode;
  const step = %d, view = '%s';
  S.user = 'alice@example.com';
  if (view) { setTimeout(() => { show(view); if (view === 'about') checkUpdate(); }, 600); }
  else if (step === 3) { S.step = 3; render(); }
  else if (step === 4) { S.step = 4; S.registered = [{ name: 'Claude Desktop', ok: true }, { name: 'Claude Code', ok: true }]; render(); }
  else { go(step); }
  if (step === 2) setTimeout(() => { document.querySelector('#pass').value = 'bridge-password'; document.activeElement?.blur(); render(); }, 50);
}, 300));`, zoom, height, mode, step, map[bool]string{true: page}[app]))
	w.SetHtml(wizard.Page)
	w.Run()
}
