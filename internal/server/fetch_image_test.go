package server

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// stubFetcher answers every address with data or err and records what was
// asked for. Nothing reaches the network.
type stubFetcher struct {
	mu   sync.Mutex
	urls []string
	data []byte
	err  error
}

func (f *stubFetcher) Get(_ context.Context, url string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.urls = append(f.urls, url)
	return f.data, f.err
}

func (f *stubFetcher) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

const remoteMsg = "From: Bob <bob@example.org>\r\nTo: alice@example.com\r\nSubject: News\r\n" +
	"Date: Mon, 02 Nov 2026 10:00:00 +0000\r\nContent-Type: text/html\r\n\r\n" +
	`<p>Hi</p><img src="https://cdn.example.org/one.png" alt="Logo"><p>Book</p><img src="https://cdn.example.org/two.png">` + "\r\n"

// fetch_image fetches exactly the address of the image asked for, and shows
// it; the message stays unread.
func TestFetchImage(t *testing.T) {
	t.Parallel()
	f := &stubFetcher{data: pngBytes(t, 30, 10)}
	e := newEnvImages(t, nil, f)
	e.imap.AddMessage(t, "INBOX", remoteMsg)

	var msg ReadMessageOutput
	e.callOK(t, "read_message", map[string]any{"folder": "INBOX", "uid": 1}, &msg)
	ri := msg.UntrustedContent.RemoteImages
	if len(ri) != 2 || ri[1].URL != "https://cdn.example.org/two.png" || !strings.Contains(msg.UntrustedContent.Body, "[image 2]") {
		t.Fatalf("remote_images = %+v\n%s", ri, msg.UntrustedContent.Body)
	}
	if len(f.asked()) != 0 {
		t.Fatal("read_message fetched an image")
	}

	res := e.call(t, "fetch_image", fetchArgs(t, e, 2))
	if res.IsError {
		t.Fatalf("fetch_image: %s", text(res))
	}
	var img *mcp.ImageContent
	for _, c := range res.Content {
		if i, ok := c.(*mcp.ImageContent); ok {
			img = i
		}
	}
	if img == nil || img.MIMEType != "image/png" {
		t.Fatalf("no PNG image content: %+v", res.Content)
	}
	var out FetchImageOutput
	if err := decode(res, &out); err != nil || out.Width != 30 || out.UntrustedContent.URL != ri[1].URL || !strings.Contains(out.Note, "now knows") {
		t.Errorf("output = %+v, %v", out, err)
	}
	if got := f.asked(); len(got) != 1 || got[0] != "https://cdn.example.org/two.png" {
		t.Errorf("fetched %q", got)
	}
	if e.imap.Sent(`Seen`) {
		t.Error("the message was marked read")
	}
}

// fetchArgs are fetch_image's arguments for image n of the first message in
// INBOX.
func fetchArgs(t *testing.T, e *env, n int) map[string]any {
	t.Helper()
	var l ListMessagesOutput
	e.callOK(t, "list_messages", map[string]any{"folder": "INBOX"}, &l)
	return map[string]any{"folder": "INBOX", "uid": 1, "uidvalidity": l.UIDValidity, "image": n}
}

// Off by default: refused, saying how to switch it on, and nothing fetched.
// An image number out of range, a fetch that fails or content that isn't an
// image is an error too.
func TestFetchImageRefuses(t *testing.T) {
	t.Parallel()
	off := newEnv(t)
	off.imap.AddMessage(t, "INBOX", remoteMsg)
	res := off.call(t, "fetch_image", fetchArgs(t, off, 1))
	if !res.IsError || !strings.Contains(text(res), "PROTON_ALLOW_REMOTE_IMAGES=1") {
		t.Errorf("off: %s", text(res))
	}

	for name, tc := range map[string]struct {
		f     *stubFetcher
		image int
		want  string
		calls int
	}{
		"no such image": {&stubFetcher{data: pngBytes(t, 2, 2)}, 3, "no image 3", 0},
		"fetch fails":   {&stubFetcher{err: errors.New("the image could not be fetched: it took too long")}, 1, "took too long", 1},
		"not an image":  {&stubFetcher{data: []byte("<html>surprise</html>")}, 1, "not a PNG", 1},
	} {
		e := newEnvImages(t, nil, tc.f)
		e.imap.AddMessage(t, "INBOX", remoteMsg)
		res := e.call(t, "fetch_image", fetchArgs(t, e, tc.image))
		if !res.IsError || !strings.Contains(text(res), tc.want) || len(tc.f.asked()) != tc.calls {
			t.Errorf("%s: error %v %q, %d fetches", name, res.IsError, text(res), len(tc.f.asked()))
		}
	}
}
