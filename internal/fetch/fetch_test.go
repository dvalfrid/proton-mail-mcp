package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// testLimit is the response size limit in tests.
const testLimit = 100

// testFetcher reaches only the given test servers, trusting their
// certificates; every other address is judged by Public.
func testFetcher(t *testing.T, allowed ...*httptest.Server) *Fetcher {
	t.Helper()
	f := New(testLimit)
	ok := map[netip.AddrPort]bool{}
	for _, s := range allowed {
		ok[netip.MustParseAddrPort(s.Listener.Addr().String())] = true
	}
	f.allow = func(ap netip.AddrPort) bool { return ok[ap] || Public(ap) }
	if len(allowed) > 0 {
		tr := f.client.Transport.(*http.Transport)                                                   //nolint:forcetypeassert // New sets it
		tr.TLSClientConfig = allowed[0].Client().Transport.(*http.Transport).TLSClientConfig.Clone() //nolint:forcetypeassert // httptest's client
	}
	return f
}

func serve(body string) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Referer") != "" {
			http.Error(w, "cookie or referer sent", http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
}

func TestGet(t *testing.T) {
	t.Parallel()
	srv := serve("image bytes")
	defer srv.Close()
	data, err := testFetcher(t, srv).Get(context.Background(), srv.URL+"/a.png")
	if err != nil || string(data) != "image bytes" {
		t.Fatalf("Get = %q, %v", data, err)
	}
}

// A message can't make the server reach this computer or its network.
func TestGetRefusesLocalAddresses(t *testing.T) {
	t.Parallel()
	srv := serve("x")
	defer srv.Close()
	ctx := context.Background()
	f := New(100) // the real check
	for _, u := range []string{
		srv.URL + "/a.png", // 127.0.0.1
		"https://localhost:1143/", "https://[::1]/", "https://10.0.0.1/", "https://192.168.1.1/",
		"https://169.254.169.254/latest/meta-data/", "https://[fe80::1]/", "https://0.0.0.0/",
	} {
		if _, err := f.Get(ctx, u); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: err = %v, want ErrNotAllowed", u, err)
		}
	}
	for _, u := range []string{"http://example.com/a.png", "ftp://example.com/a", "https://user:pw@example.com/a", "//example.com/a"} {
		if _, err := f.Get(ctx, u); !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: err = %v, want ErrNotAllowed", u, err)
		}
	}
}

// A redirect is checked like the first address: to a server not allowed,
// or to http, it is refused.
func TestGetRefusesBadRedirects(t *testing.T) {
	t.Parallel()
	inner := serve("x") // stands for an address on the local network
	defer inner.Close()
	for name, target := range map[string]string{
		"to a private address": inner.URL + "/a.png",
		"to http":              "http://example.com/a.png",
	} {
		outer := httptest.NewTLSServer(http.RedirectHandler(target, http.StatusFound))
		_, err := testFetcher(t, outer).Get(context.Background(), outer.URL+"/start")
		outer.Close()
		if !errors.Is(err, ErrNotAllowed) {
			t.Errorf("%s: err = %v, want ErrNotAllowed", name, err)
		}
	}
	loop := httptest.NewTLSServer(nil)
	loop.Config.Handler = http.RedirectHandler(loop.URL+"/again", http.StatusFound)
	defer loop.Close()
	if _, err := testFetcher(t, loop).Get(context.Background(), loop.URL); !errors.Is(err, ErrFailed) {
		t.Errorf("redirect loop: err = %v, want ErrFailed", err)
	}
}

func TestGetLimits(t *testing.T) {
	t.Parallel()
	big := serve(strings.Repeat("x", 101))
	defer big.Close()
	if _, err := testFetcher(t, big).Get(context.Background(), big.URL); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large: err = %v", err)
	}
	chunked := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for range 5 {
			_, _ = w.Write([]byte(strings.Repeat("x", 40)))
			w.(http.Flusher).Flush() //nolint:forcetypeassert // httptest's writer flushes
		}
	}))
	defer chunked.Close()
	if _, err := testFetcher(t, chunked).Get(context.Background(), chunked.URL); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too large, no length: err = %v", err)
	}
	missing := httptest.NewTLSServer(http.NotFoundHandler())
	defer missing.Close()
	_, err := testFetcher(t, missing).Get(context.Background(), missing.URL+"/sender-written-path")
	if !errors.Is(err, ErrFailed) || strings.Contains(err.Error(), "sender-written-path") {
		t.Errorf("404: err = %v, want ErrFailed without the address", err)
	}
}

func TestPublic(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]bool{
		"93.184.215.14:443": true, "[2606:2800:21f:cb07:6820:80da:af6b:8b2c]:443": true,
		"93.184.215.14:80": false, "127.0.0.1:443": false, "10.1.2.3:443": false, "172.16.0.1:443": false,
		"192.168.0.1:443": false, "169.254.169.254:443": false, "100.64.0.1:443": false, "0.0.0.0:443": false,
		"255.255.255.255:443": false, "224.0.0.1:443": false, "[::1]:443": false, "[fc00::1]:443": false,
		"[fe80::1]:443": false, "[::ffff:127.0.0.1]:443": false, "[::ffff:10.0.0.1]:443": false,
		"[64:ff9b::a00:1]:443": false, "[2002:a00:1::]:443": false, "[2001:db8::1]:443": false,
	} {
		if got := Public(netip.MustParseAddrPort(in)); got != want {
			t.Errorf("Public(%s) = %v, want %v", in, got, want)
		}
	}
}

// Only this package reaches the internet: no other program code imports
// net/http or opens its own connections (tests don't count). A new one must
// be added here deliberately (STANDARDS.md#remote-images).
func TestNothingElseFetches(t *testing.T) {
	t.Parallel()
	network := regexp.MustCompile(`"net/http"|"net/http/[a-z]+"|net\.Dial\(|net\.DialTimeout\(|\.DialContext\(|net\.Dialer\{|"net/rpc"|"net/smtp"`)
	allowed := map[string]bool{
		"internal/fetch/fetch.go": true,
		// Bridge on localhost only, never the internet (STANDARDS.md#transport):
		"internal/mail/client.go":     true, // the IMAP connection
		"internal/bridge/probe.go":    true, // setup and check recognising Bridge
		"internal/testutil/bridge.go": true, // tests only
	}
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "." && rel != "internal" && rel != "cmd" && !strings.HasPrefix(rel, "internal/") && !strings.HasPrefix(rel, "cmd/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, "_test.go") || allowed[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := network.Find(src); m != nil {
			t.Errorf("%s makes network connections (%s); only internal/fetch may reach the internet", rel, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
