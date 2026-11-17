package wizard

import (
	"context"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

// checkUpdate asks GitHub for the latest release, only when the owner clicks
// Check for updates (issue #159, STANDARDS.md#update-check). The release
// page it finds is the one openLink("release") then opens.
func (b *Backend) checkUpdate(ctx context.Context) (update.Result, error) {
	r, err := update.Check(ctx, b.updates, b.version)
	if err != nil {
		return update.Result{}, err
	}
	b.releaseURL = r.ReleaseURL
	return r, nil
}

// releasePage is the release the last check found, else the list of
// releases. Its address is built by fetch from the tag, never taken from
// GitHub's answer.
func (b *Backend) releasePage() string {
	if b.releaseURL != "" {
		return b.releaseURL
	}
	return fetch.ReleasesPage
}
