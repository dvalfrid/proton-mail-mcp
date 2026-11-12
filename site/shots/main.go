//go:build ignore

// shots shows the setup wizard's real page (internal/wizard/page.html) at
// one step, with invented data (alice@example.com) instead of a real
// backend, in a window titled "pmcp-shot", for site/shots/shots.ps1 to
// capture. Windows only (WebView2). Usage:
//
//	go run site/shots/main.go <step 0-4> <light|dark>
package main

import (
	"fmt"
	"log"
	"os"
	"strconv"

	webview2 "github.com/jchv/go-webview2"

	"github.com/dvalfrid/proton-mail-mcp/internal/wizard"
)

// Twice the wizard's 780x560, for sharp images on high-density screens.
const zoom, width, height = 2, 780, 560

// mock answers the page's calls with invented data.
const mock = `
window.__pmcp = (id, method, args) => {
  const reply = v => setTimeout(() => window.__pmcpDone(id, true, v), 0);
  switch (method) {
    case 'state': reply({ platform: 'windows', version: '0.3.0', user: 'alice@example.com', savedPassword: false,
      store: 'Windows Credential Manager', canVerifyOwner: true, accent: {},
      exe: 'C:\\Program Files\\proton-mail-mcp\\proton-mail-mcp.exe',
      clients: [{ name: 'Claude Desktop', registered: false }, { name: 'Claude Code', registered: false }] }); break;
    case 'findBridge': reply({ addr: '127.0.0.1:1143', answers: true, isBridge: true, installed: true,
      fingerprint: '5C:1E:9A:04:7D:B2:63:F0:8A:21:CE:47:93:0B:D8:6F:12:A5:3E:C9:70:4D:E1:88:2B:96:F4:0C:57:A3:1D:6E',
      looksLikeBridge: true, ownerKnown: true, ownerPath: 'C:\\Program Files\\Proton AG\\Proton Mail Bridge\\bridge.exe', verified: true }); break;
    default: reply(null);
  }
  return Promise.resolve();
};`

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: go run site/shots/main.go <step 0-4> <light|dark>")
	}
	step, err := strconv.Atoi(os.Args[1])
	if err != nil || step < 0 || step > 4 {
		log.Fatal("step is 0 to 4")
	}
	mode := os.Args[2]

	w := webview2.NewWithOptions(webview2.WebViewOptions{WindowOptions: webview2.WindowOptions{Title: "pmcp-shot", Center: true}})
	if w == nil {
		log.Fatal("WebView2 is not available")
	}
	defer w.Destroy()
	w.SetSize(width*zoom, height*zoom, webview2.HintFixed)
	w.Init(mock)
	// After the page has loaded its state: the mode, the zoom, and the step.
	w.Init(fmt.Sprintf(`addEventListener('load', () => setTimeout(() => {
  document.documentElement.style.zoom = '%d';
  document.querySelector('.window').style.height = '%dpx';
  const mode = () => { document.body.className = 'win %s'; };
  mode(); theme = mode;
  const step = %d;
  S.user = 'alice@example.com';
  if (step === 3) { S.step = 3; render(); }
  else if (step === 4) { S.step = 4; S.registered = [{ name: 'Claude Desktop', ok: true }, { name: 'Claude Code', ok: true }]; render(); }
  else { go(step); }
  if (step === 2) setTimeout(() => { document.querySelector('#pass').value = 'bridge-password'; document.activeElement?.blur(); render(); }, 50);
}, 300));`, zoom, height, mode, step))
	w.SetHtml(wizard.Page)
	w.Run()
}
