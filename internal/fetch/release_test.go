package fetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// releaseServer answers as GitHub's releases/latest would, with body.
func releaseServer(t *testing.T, body string) *Fetcher {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "" {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	f := testFetcher(t, srv)
	f.latestURL = srv.URL + "/repos/dvalfrid/proton-mail-mcp/releases/latest"
	return f
}

func TestLatest(t *testing.T) {
	t.Parallel()
	f := releaseServer(t, `{"tag_name":"v0.6.0","html_url":"https://example.org/elsewhere","draft":false,"prerelease":false,"body":"notes"}`)
	rel, err := f.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// The link is built from the tag, never taken from the answer.
	if rel.Version != "0.6.0" || rel.URL != ReleasesPage+"/tag/v0.6.0" {
		t.Errorf("Latest = %+v", rel)
	}
}

// Only a plain release counts; anything else is "couldn't check".
func TestLatestRefusesOdd(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"tag_name":"v0.6.0-rc.1","prerelease":true}`,
		`{"tag_name":"v0.6.0-rc.1"}`,
		`{"tag_name":"v0.6.0","draft":true}`,
		`{"tag_name":"0.6.0"}`,
		`{"tag_name":"v0.6.0/../../x"}`,
		`{}`,
		`not json`,
	} {
		f := releaseServer(t, body)
		if rel, err := f.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
			t.Errorf("Latest(%s) = %+v, %v; want ErrNoRelease", body, rel, err)
		}
	}
}

// Offline, or an error from GitHub: ErrNoRelease, worded without images.
func TestLatestFails(t *testing.T) {
	t.Parallel()
	missing := httptest.NewTLSServer(http.NotFoundHandler())
	defer missing.Close()
	f := testFetcher(t, missing)
	f.latestURL = missing.URL + "/latest"
	_, err := f.Latest(context.Background())
	if !errors.Is(err, ErrNoRelease) || strings.Contains(err.Error(), "image") || !strings.Contains(err.Error(), "404") {
		t.Errorf("404: err = %v", err)
	}

	f = releaseServer(t, strings.Repeat("x", maxReleaseBytes+1))
	if _, err := f.Latest(context.Background()); !errors.Is(err, ErrNoRelease) || strings.Contains(err.Error(), "image") {
		t.Errorf("too large: err = %v", err)
	}

	// A local address is refused even for the fixed URL's host.
	f = New(testLimit)
	f.latestURL = missing.URL + "/latest"
	if _, err := f.Latest(context.Background()); !errors.Is(err, ErrNoRelease) {
		t.Errorf("local address: err = %v", err)
	}
}

// The address asked is the fixed one: no part of it comes from elsewhere.
func TestLatestURLIsFixed(t *testing.T) {
	t.Parallel()
	if got := New(1).latestURL; got != "https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases/latest" {
		t.Errorf("latestURL = %q", got)
	}
}

// listServer answers as GitHub's list of releases would, with body.
func listServer(t *testing.T, body string) *Fetcher {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.URL.RawQuery != "per_page=20" {
			http.Error(w, "unexpected request", http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	f := testFetcher(t, srv)
	f.releasesURL = srv.URL + "/repos/dvalfrid/proton-mail-mcp/releases?per_page=20"
	return f
}

// The test channel's list: releases and release candidates, by tag only;
// drafts and odd tags are left out (issue #213).
func TestReleases(t *testing.T) {
	t.Parallel()
	f := listServer(t, `[
		{"tag_name":"v0.8.0","draft":true},
		{"tag_name":"v0.7.0-rc.1","prerelease":true,"html_url":"https://example.org/elsewhere"},
		{"tag_name":"v0.7.0-beta","prerelease":true},
		{"tag_name":"v0.6.0/../x"},
		{"tag_name":"v0.6.0"}]`)
	got, err := f.Releases(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []Release{
		{Version: "0.7.0-rc.1", URL: ReleasesPage + "/tag/v0.7.0-rc.1", Candidate: true},
		{Version: "0.6.0", URL: ReleasesPage + "/tag/v0.6.0"},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("Releases = %+v, want %+v", got, want)
	}

	for _, body := range []string{`[]`, `[{"tag_name":"v1.0.0","draft":true}]`, `{"tag_name":"v0.6.0"}`, `not json`} {
		if rel, err := listServer(t, body).Releases(context.Background()); !errors.Is(err, ErrNoRelease) {
			t.Errorf("Releases(%s) = %+v, %v; want ErrNoRelease", body, rel, err)
		}
	}
}

// The test channel's address is fixed too.
func TestReleasesURLIsFixed(t *testing.T) {
	t.Parallel()
	if got := New(1).releasesURL; got != "https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases?per_page=20" {
		t.Errorf("releasesURL = %q", got)
	}
}
