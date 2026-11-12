//go:build ignore

// genicons draws icon.svg as PNGs and a Windows .ico, using WebView2 (so it
// runs on Windows). Run it from the repository root after changing the SVG:
//
//	go run assets/genicons.go
//
// It writes assets/png/icon-<size>.png, assets/icon.ico, and copies the
// small sizes the setup wizard's window uses into internal/wizard/icons.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	webview2 "github.com/jchv/go-webview2"
)

// Sizes drawn: the .ico's (Windows uses 16 to 256), and larger ones for the
// web.
var (
	icoSizes    = []int{16, 20, 24, 32, 40, 48, 64, 128, 256}
	webSizes    = []int{512}
	wizardSizes = []int{16, 20, 24, 32, 40, 48, 64}
)

func main() {
	svg, err := os.ReadFile("assets/icon.svg")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll("assets/png", 0o755); err != nil {
		log.Fatal(err)
	}
	pngs := map[int][]byte{}

	w := webview2.NewWithOptions(webview2.WebViewOptions{WindowOptions: webview2.WindowOptions{Title: "genicons", Width: 200, Height: 200}})
	if w == nil {
		log.Fatal("WebView2 is not available")
	}
	defer w.Destroy()
	if err := w.Bind("save", func(size int, b64 string) error {
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return err
		}
		pngs[size] = data
		return os.WriteFile(filepath.Join("assets", "png", fmt.Sprintf("icon-%d.png", size)), data, 0o644)
	}); err != nil {
		log.Fatal(err)
	}
	if err := w.Bind("done", func() { w.Terminate() }); err != nil {
		log.Fatal(err)
	}
	sizes, _ := json.Marshal(slices.Concat(icoSizes, webSizes))
	svgJS, _ := json.Marshal(string(svg))
	w.SetHtml(`<!DOCTYPE html><html><body><script>
(async () => {
  const img = new Image();
  img.src = 'data:image/svg+xml;charset=utf-8,' + encodeURIComponent(` + string(svgJS) + `);
  await img.decode();
  for (const size of ` + string(sizes) + `) {
    const c = document.createElement('canvas');
    c.width = c.height = size;
    const ctx = c.getContext('2d');
    ctx.imageSmoothingQuality = 'high';
    ctx.drawImage(img, 0, 0, size, size);
    await save(size, c.toDataURL('image/png').split(',')[1]);
  }
  done();
})();
</script></body></html>`)
	go func() {
		time.Sleep(30 * time.Second)
		log.Fatal("timed out")
	}()
	w.Run()

	for _, s := range slices.Concat(icoSizes, webSizes) {
		if pngs[s] == nil {
			log.Fatalf("size %d was not drawn", s)
		}
	}
	if err := os.WriteFile("assets/icon.ico", ico(pngs), 0o644); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll("internal/wizard/icons", 0o755); err != nil {
		log.Fatal(err)
	}
	for _, s := range wizardSizes {
		if err := os.WriteFile(filepath.Join("internal", "wizard", "icons", fmt.Sprintf("icon-%d.png", s)), pngs[s], 0o644); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("wrote assets/png, assets/icon.ico and internal/wizard/icons")
}

// ico packs PNG images into an .ico file (PNG entries, Windows Vista and
// later).
func ico(pngs map[int][]byte) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) } // a bytes.Buffer doesn't fail
	le(uint16(0))
	le(uint16(1)) // icon
	le(uint16(len(icoSizes)))
	offset := 6 + 16*len(icoSizes)
	for _, s := range icoSizes {
		dim := uint8(s)
		if s >= 256 {
			dim = 0 // 0 means 256
		}
		le(dim)
		le(dim)
		le(uint8(0))  // no palette
		le(uint8(0))  // reserved
		le(uint16(1)) // planes
		le(uint16(32))
		le(uint32(len(pngs[s])))
		le(uint32(offset))
		offset += len(pngs[s])
	}
	for _, s := range icoSizes {
		b.Write(pngs[s])
	}
	return b.Bytes()
}
