package fetch

import (
	"context"
	"os"
	"testing"
)

// TestLiveLatest asks the real GitHub, once. Only with
// PROTON_LIVE_UPDATE=1: tests otherwise never reach the internet.
func TestLiveLatest(t *testing.T) {
	if os.Getenv("PROTON_LIVE_UPDATE") != "1" {
		t.Skip("set PROTON_LIVE_UPDATE=1 to ask GitHub for the latest release")
	}
	rel, err := New(0).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("latest release %s, %s", rel.Version, rel.URL)
}
