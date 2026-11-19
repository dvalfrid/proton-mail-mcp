package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The update check asks one of two fixed addresses, and nothing from a
// message or from Claude goes into it (STANDARDS.md#update-check).
// releases/latest never returns a pre-release or a draft; the list, asked
// only in the test channel the owner switched on (issue #213), holds
// release candidates too.
const (
	latestURL   = "https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases/latest"
	releasesURL = "https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases?per_page=20"
	// ReleasesPage lists every release, where the owner downloads one.
	ReleasesPage = "https://github.com/dvalfrid/proton-mail-mcp/releases"
	// maxReleaseBytes caps GitHub's answer: its release notes and asset
	// list are a few tens of KiB.
	maxReleaseBytes = 1 << 20
)

// ErrNoRelease means the latest release could not be found out: offline,
// GitHub failed, or its answer made no sense.
var ErrNoRelease = errors.New("couldn't check for updates")

// releaseTag is a release's tag: v and a version with no pre-release part.
var releaseTag = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

// candidateTag is a release's or a release candidate's tag: v, a version,
// and an optional -rc.N.
var candidateTag = regexp.MustCompile(`^v(\d+\.\d+\.\d+(?:-rc\.\d+)?)$`)

// Release is a release of proton-mail-mcp.
type Release struct {
	Version string // e.g. 0.5.0 or 0.7.0-rc.1, without the v
	URL     string // its release page, built here from the tag, never taken from the answer
	// Candidate marks a release candidate (a -rc.N tag), only from Releases.
	Candidate bool
}

// Latest asks GitHub for the latest release. It sends no cookies or tokens,
// only a User-Agent, and reads only the tag. Nothing is downloaded.
func (f *Fetcher) Latest(ctx context.Context) (Release, error) {
	data, err := f.get(ctx, f.latestURL, "application/vnd.github+json", maxReleaseBytes)
	if err != nil {
		return Release{}, fmt.Errorf("%w: %s", ErrNoRelease, cause(err))
	}
	var r struct {
		Tag        string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return Release{}, fmt.Errorf("%w: GitHub's answer can't be read", ErrNoRelease)
	}
	m := releaseTag.FindStringSubmatch(r.Tag)
	if m == nil || r.Draft || r.Prerelease {
		return Release{}, fmt.Errorf("%w: GitHub's answer holds no release", ErrNoRelease)
	}
	return Release{Version: m[1], URL: ReleasesPage + "/tag/" + r.Tag}, nil
}

// Releases asks GitHub for the newest releases and release candidates, for
// the test channel. Like Latest it sends no cookies or tokens and reads
// only the tags; drafts and tags of any other form are left out.
func (f *Fetcher) Releases(ctx context.Context) ([]Release, error) {
	data, err := f.get(ctx, f.releasesURL, "application/vnd.github+json", maxReleaseBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoRelease, cause(err))
	}
	var list []struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("%w: GitHub's answer can't be read", ErrNoRelease)
	}
	var out []Release
	for _, r := range list {
		m := candidateTag.FindStringSubmatch(r.Tag)
		if m == nil || r.Draft {
			continue
		}
		out = append(out, Release{Version: m[1], URL: ReleasesPage + "/tag/" + r.Tag, Candidate: strings.Contains(m[1], "-rc.")})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: GitHub's answer holds no release", ErrNoRelease)
	}
	return out, nil
}

// cause is err's reason without the image wording of this package's own
// errors.
func cause(err error) string {
	msg := err.Error()
	for _, s := range []error{ErrNotAllowed, ErrTooLarge, ErrFailed} {
		if errors.Is(err, s) {
			return strings.TrimPrefix(strings.TrimPrefix(msg, s.Error()), ": ")
		}
	}
	return "the connection failed"
}
