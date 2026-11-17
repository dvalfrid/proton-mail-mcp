package check

import (
	"errors"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/keystore"
)

// A credential store that fails is reported as such, not as a missing
// password; a password that simply isn't stored is the usual error (#74).
func TestLoadWithStoreReportsStoreFailure(t *testing.T) {
	t.Parallel()
	files := config.FromMap(map[string]string{config.EnvUser: "alice@example.com", config.EnvCert: "bridge.pem"})
	_, err := loadWithStore(files, func(string) (string, error) { return "", errors.New("the keychain is locked") })
	if err == nil || !strings.Contains(err.Error(), "credential store could not be read") || !strings.Contains(err.Error(), "keychain is locked") {
		t.Errorf("store failure: err = %v", err)
	}
	_, err = loadWithStore(files, func(string) (string, error) { return "", keystore.ErrNotFound })
	if err == nil || strings.Contains(err.Error(), "credential store could not be read") {
		t.Errorf("no password stored: err = %v", err)
	}
}
