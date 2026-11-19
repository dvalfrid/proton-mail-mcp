package wizard

import (
	"context"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"

	"github.com/dvalfrid/proton-mail-mcp/internal/fetch"
	"github.com/dvalfrid/proton-mail-mcp/internal/update"
)

// checkUpdate asks GitHub for the latest release, only when the owner clicks
// Check for updates (issue #159, STANDARDS.md#update-check). The release
// page it finds is the one openLink("release") then opens.
func (b *Backend) checkUpdate(ctx context.Context) (update.Result, error) {
	r, err := update.Check(ctx, update.ForChannel(b.updates, b.updateChannel()), b.version)
	if err != nil {
		return update.Result{}, err
	}
	b.releaseURL = r.ReleaseURL
	return r, nil
}

// updateChannel is the owner's update channel as the server reads it: the
// environment, then .env, then config.json.
func (b *Backend) updateChannel() string {
	saved, _ := config.ReadSettings(config.SettingsFile(b.o.Dir)) // unreadable: the stable channel
	v, _ := config.Chain(config.Lookup(b.dotEnv()), config.FromMap(saved.Values()))(config.EnvUpdateChannel)
	if strings.EqualFold(strings.TrimSpace(v), config.ChannelTest) {
		return config.ChannelTest
	}
	return config.ChannelStable
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
