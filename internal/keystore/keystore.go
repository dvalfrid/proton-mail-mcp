// Package keystore keeps the Bridge password in the operating system's
// credential store: Windows Credential Manager, the macOS Keychain, or the
// Secret Service on Linux (through github.com/zalando/go-keyring).
//
// Known weakness on macOS: go-keyring creates items through
// /usr/bin/security, so any program running as the same user can read the
// item without a prompt (docs/installation-research.md §D1). That is no
// worse than a file only the user can read, and it is stated in SECURITY.md.
package keystore

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

// Service is the name the password is stored under, with the Bridge
// username as the account.
const Service = "proton-mail-mcp"

// ErrNotFound means no password is stored for the user.
var ErrNotFound = errors.New("no Bridge password in the credential store")

// Store keeps one password per Bridge username.
type Store interface {
	Get(user string) (string, error)
	Set(user, password string) error
}

// OS is the operating system's credential store.
type OS struct{}

// Get returns the stored password, or ErrNotFound.
func (OS) Get(user string) (string, error) {
	pw, err := keyring.Get(Service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("read the credential store: %w", err)
	}
	return pw, nil
}

// Set stores the password, replacing any earlier one.
func (OS) Set(user, password string) error {
	if err := keyring.Set(Service, user, password); err != nil {
		return fmt.Errorf("write the credential store: %w", err)
	}
	return nil
}

// Memory is a Store for tests.
type Memory map[string]string

// Get returns the password, or ErrNotFound.
func (m Memory) Get(user string) (string, error) {
	pw, ok := m[user]
	if !ok {
		return "", ErrNotFound
	}
	return pw, nil
}

// Set stores the password.
func (m Memory) Set(user, password string) error {
	m[user] = password
	return nil
}
