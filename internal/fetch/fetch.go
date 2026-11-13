// Package fetch gets an image a message links to on the internet, for
// fetch_image. It is the only package in the server that makes an HTTP
// request (depguard): the one exception to "nothing is fetched", approved
// by the owner on 2026-11-13 (issue #150, STANDARDS.md#remote-images).
//
// The address comes from a sender, so the request is fenced in: https on
// port 443 only, to public addresses only, checked on every connection
// (redirects and fresh DNS answers included, so DNS rebinding doesn't get
// past it), no proxy, no cookies, no Referer, a size and time limit.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Limits on a fetch.
const (
	Timeout      = 15 * time.Second
	MaxRedirects = 3
	userAgent    = "proton-mail-mcp"
)

var (
	// ErrNotAllowed means the address is not one the server fetches from:
	// not https on port 443, or not a public address.
	ErrNotAllowed = errors.New("the image's address is not allowed")
	// ErrTooLarge means the response is longer than the limit.
	ErrTooLarge = errors.New("the image is too large to fetch")
	// ErrFailed means the server answered with an error or not at all.
	ErrFailed = errors.New("the image could not be fetched")
)

// Fetcher fetches images. Its zero value is not usable: use New.
type Fetcher struct {
	client *http.Client
	limit  int64
	// allow decides whether a connection may be made to an address; tests
	// replace it to reach a local test server.
	allow func(netip.AddrPort) bool
}

// New returns a Fetcher that reads at most limit bytes of a response.
func New(limit int64) *Fetcher {
	f := &Fetcher{limit: limit, allow: Public}
	dialer := &net.Dialer{
		Timeout: Timeout,
		// Control runs for every connection, after DNS, with the address
		// actually dialled: the check can't be fooled by a name that
		// resolves differently the second time.
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !f.allow(ap) {
				return fmt.Errorf("%w: it leads to a local or private network address, or a port other than 443", ErrNotAllowed)
			}
			return nil
		},
	}
	f.client = &http.Client{
		Timeout: Timeout,
		Transport: &http.Transport{
			Proxy:                  nil, // a proxy would dial for us, past the address check
			DialContext:            dialer.DialContext,
			TLSHandshakeTimeout:    Timeout,
			ResponseHeaderTimeout:  Timeout,
			MaxResponseHeaderBytes: 64 << 10,
			ForceAttemptHTTP2:      true,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > MaxRedirects {
				return fmt.Errorf("%w: more than %d redirects", ErrFailed, MaxRedirects)
			}
			return checkURL(req.URL)
		},
	}
	return f
}

// Get fetches rawURL and returns its body. Only https on port 443 to a
// public address is fetched; the response must be 200 and at most the
// limit. Nothing is written to disk.
func (f *Fetcher) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: it can't be parsed", ErrNotAllowed)
	}
	if err := checkURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotAllowed, err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/png, image/jpeg, image/gif, image/webp")

	resp, err := f.client.Do(req)
	if err != nil {
		if own := ownError(err); own != nil {
			return nil, own
		}
		return nil, fmt.Errorf("%w: %s", ErrFailed, reason(err))
	}
	defer func() { _ = resp.Body.Close() }() // read-only; nothing to lose

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the server answered %d", ErrFailed, resp.StatusCode)
	}
	if resp.ContentLength > f.limit {
		return nil, fmt.Errorf("%w: %d bytes, the limit is %d", ErrTooLarge, resp.ContentLength, f.limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, f.limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrFailed, reason(err))
	}
	if int64(len(data)) > f.limit {
		return nil, fmt.Errorf("%w: over %d bytes", ErrTooLarge, f.limit)
	}
	return data, nil
}

// checkURL allows https with no user name or password. The port is checked
// when dialling (Public).
func checkURL(u *url.URL) error {
	switch {
	case u.Scheme != "https":
		return fmt.Errorf("%w: only https addresses are fetched", ErrNotAllowed)
	case u.User != nil:
		return fmt.Errorf("%w: it contains a user name", ErrNotAllowed)
	case u.Hostname() == "":
		return fmt.Errorf("%w: it has no host", ErrNotAllowed)
	}
	return nil
}

// blocked are address ranges that are not the public internet, beyond
// what netip's own predicates cover.
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this network"
	netip.MustParsePrefix("100.64.0.0/10"),   // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, and broadcast
	netip.MustParsePrefix("64:ff9b::/96"),    // NAT64: can map to a private IPv4 address
	netip.MustParsePrefix("64:ff9b:1::/48"),  // local-use NAT64
	netip.MustParsePrefix("2001:db8::/32"),   // documentation
	netip.MustParsePrefix("2002::/16"),       // 6to4: embeds any IPv4 address
	netip.MustParsePrefix("2001::/32"),       // Teredo: embeds an IPv4 address
}

// Public reports whether ap is an address on the public internet, on port
// 443: not loopback, private, link-local, multicast, unspecified or a
// reserved range. An IPv4-mapped IPv6 address is judged as IPv4.
func Public(ap netip.AddrPort) bool {
	a := ap.Addr().Unmap()
	if ap.Port() != 443 || !a.IsValid() || !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() ||
		a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() {
		return false
	}
	for _, p := range blocked {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// ownError finds the error this package made inside err (a *url.Error,
// a *net.OpError): its message gives the reason without the address,
// which the sender wrote.
func ownError(err error) error {
	for e := err; e != nil; e = errors.Unwrap(e) {
		for _, s := range []error{ErrNotAllowed, ErrFailed} {
			if errors.Is(e, s) && strings.HasPrefix(e.Error(), s.Error()) {
				return e
			}
		}
	}
	return nil
}

// reason is a short cause for a failed request, without the address.
func reason(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "it took too long"
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return "the host name was not found"
	}
	return "the connection failed"
}
