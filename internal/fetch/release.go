package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// The update check asks one fixed address, and nothing from a message or
// from Claude goes into it (STANDARDS.md#update-check). releases/latest
// never returns a pre-release or a draft.
const (
	latestURL = "https://api.github.com/repos/dvalfrid/proton-mail-mcp/releases/latest"
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

// Release is the latest release of proton-mail-mcp.
type Release struct {
	Version string // e.g. 0.5.0, without the v
	URL     string // its release page, built here from the tag, never taken from the answer
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
