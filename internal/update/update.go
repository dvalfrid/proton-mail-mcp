// Package update tells the owner whether a newer release of proton-mail-mcp
// exists and how to install it. Only on request: the proton_mail_mcp_update
// tool and the Check for updates button on the Settings page (issue #125,
// STANDARDS.md#update-check). It never downloads or installs anything.
package update

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
)

// Source finds the latest release: fetch.Fetcher.
type Source interface {
	Latest(ctx context.Context) (fetch.Release, error)
}

// Result is the answer to "is there an update?".
type Result struct {
	Running         string `json:"running" jsonschema:"the version running now"`
	Latest          string `json:"latest" jsonschema:"the latest release; pre-releases are never offered"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url" jsonschema:"the latest release's page, where its files are downloaded"`
	Message         string `json:"message" jsonschema:"the answer in one sentence, for the owner"`
	HowToUpdate     string `json:"how_to_update,omitempty"`
}

// Check asks src for the latest release and compares it with running.
func Check(ctx context.Context, src Source, running string) (Result, error) {
	rel, err := src.Latest(ctx)
	if err != nil {
		return Result{}, err
	}
	r := Result{Running: running, Latest: rel.Version, ReleaseURL: rel.URL}
	order, ok := Compare(running, rel.Version)
	switch {
	case !ok:
		r.Message = fmt.Sprintf("This build has no release version (%s); the latest release is %s.", running, rel.Version)
	case order < 0:
		r.UpdateAvailable = true
		r.Message = fmt.Sprintf("Proton Mail MCP %s is available; you have %s.", rel.Version, strings.TrimPrefix(running, "v"))
		r.HowToUpdate = HowTo(rel.Version)
	case order == 0:
		r.Message = fmt.Sprintf("You have the latest version (%s).", rel.Version)
	default:
		r.Message = fmt.Sprintf("You have %s, a test version newer than the latest release (%s).", strings.TrimPrefix(running, "v"), rel.Version)
	}
	return r, nil
}

// HowTo says how to install version (empty: any) on this computer.
func HowTo(version string) string {
	if version == "" {
		version = "<version>"
	}
	file := fmt.Sprintf("proton-mail-mcp_%s_%s_%s", version, runtime.GOOS, runtime.GOARCH)
	if runtime.GOOS == "windows" {
		return "Download " + file + "_setup.exe from the release page (" + fetch.ReleasesPage + ") and run it: " +
			"it replaces the program and keeps the settings and the saved password. Then restart Claude."
	}
	return "Download " + file + ".tar.gz from the release page (" + fetch.ReleasesPage + "), " +
		"replace the proton-mail-mcp program with the one in it, then restart Claude."
}

// Compare orders two versions (an optional v, then major.minor.patch, then
// an optional -pre-release part, which comes before the release itself):
// -1, 0 or 1. ok is false when either can't be read, e.g. "dev".
func Compare(a, b string) (order int, ok bool) {
	pa, oka := parse(a)
	pb, okb := parse(b)
	if !oka || !okb {
		return 0, false
	}
	for i := range 3 {
		if pa.n[i] != pb.n[i] {
			return sign(pa.n[i] - pb.n[i]), true
		}
	}
	switch {
	case pa.pre == pb.pre:
		return 0, true
	case pa.pre == "":
		return 1, true
	case pb.pre == "":
		return -1, true
	}
	return comparePre(pa.pre, pb.pre), true
}

type version struct {
	n   [3]int
	pre string
}

func parse(s string) (version, bool) {
	s = strings.TrimPrefix(s, "v")
	core, pre, _ := strings.Cut(s, "-")
	core, _, _ = strings.Cut(core, "+") // build metadata doesn't order
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var v version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		v.n[i] = n
	}
	v.pre, _, _ = strings.Cut(pre, "+")
	return v, true
}

// comparePre orders pre-release parts as semver does: dot by dot, numbers
// as numbers and before words, and fewer parts first.
func comparePre(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) && i < len(bs); i++ {
		na, ea := strconv.Atoi(as[i])
		nb, eb := strconv.Atoi(bs[i])
		switch {
		case ea == nil && eb == nil:
			if na != nb {
				return sign(na - nb)
			}
		case ea == nil:
			return -1
		case eb == nil:
			return 1
		default:
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return c
			}
		}
	}
	return sign(len(as) - len(bs))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
