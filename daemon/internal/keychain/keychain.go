// Package keychain keeps the device key in the OS's own secret store: the Secret Service
// (GNOME Keyring, KWallet) on Linux, the Keychain on macOS, the Credential Manager on Windows.
// The key never touches a file nemd writes, and only nemd reads it.
package keychain

import (
	"errors"
	"sync"

	keyring "github.com/zalando/go-keyring"
)

// service names nem-agent's entries in the OS store; the account is the site's host.
const service = "nem-agent"

// ErrNotFound — no key stored for that site.
var ErrNotFound = errors.New("keychain: no key stored")

// Store reads and writes one secret per site.
type Store interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

// System is the OS secret store.
type System struct{}

// Get returns the stored key, or ErrNotFound.
func (System) Get(account string) (string, error) {
	s, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return s, err
}

// Set stores (or replaces) the key.
func (System) Set(account, secret string) error { return keyring.Set(service, account, secret) }

// Delete removes the key; deleting nothing is fine.
func (System) Delete(account string) error {
	err := keyring.Delete(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}

// Memory is an in-process store for tests and for `nemd --ephemeral`.
type Memory struct {
	mu sync.Mutex
	m  map[string]string
}

// NewMemory returns an empty in-memory store.
func NewMemory() *Memory { return &Memory{m: map[string]string{}} }

// Get returns the stored key, or ErrNotFound.
func (s *Memory) Get(account string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[account]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

// Set stores the key.
func (s *Memory) Set(account, secret string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[account] = secret
	return nil
}

// Delete removes the key.
func (s *Memory) Delete(account string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, account)
	return nil
}
