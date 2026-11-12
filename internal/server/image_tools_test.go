package server

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pngBytes is a w×h PNG.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		img.Set(x, 0, color.NRGBA{R: 200, A: 255})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// base64Lines is MIME base64: lines of 76 characters.
func base64Lines(data []byte) string {
	s := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s + "\r\n")
	return b.String()
}

// msgWithImage is an HTML message with an inline PNG (attachment 1, 40×20),
// a "PDF" that is text (attachment 2), and a remote image at remoteURL.
func msgWithImage(t *testing.T, remoteURL string) string {
	t.Helper()
	return "From: Bob Example <bob@example.org>\r\nTo: alice@example.com\r\nSubject: Quarterly chart\r\n" +
		"Date: Mon, 02 Nov 2026 10:00:00 +0000\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/related; boundary=b1\r\n\r\n" +
		"--b1\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<p>See the chart:</p><img src="cid:chart@example.org"><img src="` + remoteURL + `">` + "\r\n" +
		"--b1\r\nContent-Type: image/png; name=chart.png\r\nContent-ID: <chart@example.org>\r\n" +
		"Content-Disposition: inline; filename=chart.png\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		base64Lines(pngBytes(t, 40, 20)) +
		"--b1\r\nContent-Type: image/png; name=fake.png\r\nContent-Disposition: attachment; filename=fake.png\r\n\r\n" +
		"this is not an image\r\n" +
		"--b1--\r\n"
}

// The image comes back as image content Claude can see, from the message's
// own part: the remote image in the HTML is never requested.
func TestViewImage(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	remote := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	t.Cleanup(remote.Close)

	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgWithImage(t, remote.URL+"/track.png"))

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "INBOX", "uid": 1}, &msg)
	atts := msg.UntrustedContent.Attachments
	if len(atts) != 2 || atts[0].Index != 1 || atts[0].Name != "chart.png" || !atts[0].Inline || atts[1].Index != 2 {
		t.Fatalf("attachments = %+v", atts)
	}

	res := e.call(t, "view_image", map[string]any{"folder": "INBOX", "uid": 1, "attachment": 1})
	if res.IsError {
		t.Fatalf("view_image: %s", text(res))
	}
	var out ViewImageOutput
	if err := decode(res, &out); err != nil {
		t.Fatal(err)
	}
	if out.Format != "png" || out.Width != 40 || out.Height != 20 || out.Scaled || out.Note != untrustedImageNote ||
		out.UntrustedContent == nil || out.UntrustedContent.Name != "chart.png" {
		t.Errorf("result = %+v", out)
	}
	var img *mcp.ImageContent
	for _, c := range res.Content {
		if ic, ok := c.(*mcp.ImageContent); ok {
			img = ic
		}
	}
	if img == nil || img.MIMEType != "image/png" {
		t.Fatalf("no PNG image content in %+v", res.Content)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(img.Data))
	if err != nil || cfg.Width != 40 || cfg.Height != 20 {
		t.Errorf("image content: %+v, %v", cfg, err)
	}
	if hits.Load() != 0 {
		t.Errorf("the remote image was requested %d times", hits.Load())
	}
}

func TestViewImageRefuses(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.imap.AddMessage(t, "INBOX", msgWithImage(t, "http://127.0.0.1:1/x.png"))
	e.imap.AddMessage(t, "INBOX", msgFromBob)

	for _, c := range []struct {
		args map[string]any
		want string
	}{
		{map[string]any{"folder": "INBOX", "uid": 1, "attachment": 2}, "not a PNG, JPEG, GIF or WebP image"},
		{map[string]any{"folder": "INBOX", "uid": 1, "attachment": 3}, "no attachment with that index"},
		{map[string]any{"folder": "INBOX", "uid": 2, "attachment": 1}, "no attachment with that index"},
		{map[string]any{"folder": "INBOX", "uid": 1}, `missing properties: ["attachment"]`},
		{map[string]any{"folder": "INBOX", "uid": 1, "attachment": 0}, "attachment is required"},
		{map[string]any{"folder": "INBOX", "uid": 9, "attachment": 1}, "message not found"},
	} {
		res := e.call(t, "view_image", c.args)
		if !res.IsError || !strings.Contains(text(res), c.want) {
			t.Errorf("%v: IsError=%v %q, want %q", c.args, res.IsError, text(res), c.want)
		}
	}
}
