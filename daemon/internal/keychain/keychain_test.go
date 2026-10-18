package keychain

import (
	"errors"
	"os"
	"testing"
)

// TestSystemRoundTrip touches the real OS secret store, so it only runs when asked:
// NEMD_KEYCHAIN_TEST=1 go test ./internal/keychain/
func TestSystemRoundTrip(t *testing.T) {
	if os.Getenv("NEMD_KEYCHAIN_TEST") != "1" {
		t.Skip("set NEMD_KEYCHAIN_TEST=1 to use the real OS keychain")
	}
	const account = "keychain-test.invalid"
	s := System{}
	_ = s.Delete(account)
	if _, err := s.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get before Set = %v, want ErrNotFound", err)
	}
	if err := s.Set(account, "swm_test_value"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, err := s.Get(account); err != nil || v != "swm_test_value" {
		t.Fatalf("Get = %q, %v", v, err)
	}
	if err := s.Delete(account); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(account); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrNotFound", err)
	}
}

func TestMemory(t *testing.T) {
	m := NewMemory()
	if _, err := m.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_ = m.Set("a", "b")
	if v, _ := m.Get("a"); v != "b" {
		t.Fatal(v)
	}
	_ = m.Delete("a")
	if _, err := m.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}
