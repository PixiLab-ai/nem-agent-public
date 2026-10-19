// Package config holds nemd's small persistent settings and the paths it uses.
//
// The device key is NOT here: it lives in the OS keychain (package keychain). This file only
// remembers which Nemini site the device talks to, the name its key carries, and which companion
// the desktop shows.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultBase is the Nemini site every call goes through (FWS, then the API).
const DefaultBase = "https://nemini.ai"

// Config is the persisted state. Empty fields take their defaults on Load.
type Config struct {
	Base      string `json:"base"`                // the Nemini site, scheme + host
	Device    string `json:"device"`              // the name the device key carries in Settings
	Companion string `json:"companion,omitempty"` // the companion the desktop shows ("" = not chosen yet)
	KeyUUID   string `json:"key_uuid,omitempty"`  // the device key's id on the site (not the secret): what unpairing revokes
	// DeskRoot is the folder on this computer that holds a companion's desk (package desksync),
	// DeskCompanion that companion: the folder stays bound to her when the tray shows another.
	DeskRoot      string `json:"desk_root,omitempty"`
	DeskCompanion string `json:"desk_companion,omitempty"`
	// Peek: the pet notices what kind of thing the human is doing (package peek). Off by default.
	Peek bool `json:"peek,omitempty"`
}

// profile separates a second set of state on the same machine: the development app ("dev") gets
// its own config file and socket, so a dev run never changes what the installed app uses (it once
// left the installed app on the dev site). "" = the installed app's.
var profile string

// SetProfile picks the profile: lowercase letters, digits and dashes, or "" for the default.
func SetProfile(p string) error {
	for _, c := range p {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return fmt.Errorf("profile %q: only a-z, 0-9 and -", p)
		}
	}
	profile = p
	return nil
}

// suffixed is "name.ext", or "name-<profile>.ext" under a profile.
func suffixed(name, ext string) string {
	if profile == "" {
		return name + ext
	}
	return name + "-" + profile + ext
}

// Dir is nemd's config directory (created on Save).
func Dir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "nem-agent"), nil
}

// StateFile is a file of nemd's own state in its config directory (the folder sync's agreed
// state, its local trash): name + ext, apart per profile like the config itself.
func StateFile(name, ext string) (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, suffixed(name, ext)), nil
}

func file() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, suffixed("nemd", ".json")), nil
}

// Load reads the config, filling defaults. A missing file is not an error.
func Load() (*Config, error) {
	c := &Config{}
	p, err := file()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(p)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, fmt.Errorf("config %s: %w", p, err)
		}
	}
	c.fill()
	return c, nil
}

func (c *Config) fill() {
	if strings.TrimSpace(c.Base) == "" {
		c.Base = DefaultBase
	}
	c.Base = strings.TrimRight(strings.TrimSpace(c.Base), "/")
	if strings.TrimSpace(c.Device) == "" {
		c.Device = DefaultDeviceName()
	}
}

// Save writes the config atomically, readable by its owner only.
func (c *Config) Save() error {
	p, err := file()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// DefaultDeviceName — "nem-agent on <host>", what the key shows in the human's Settings.
func DefaultDeviceName() string {
	h, _ := os.Hostname()
	if h = strings.TrimSpace(h); h == "" {
		h = runtime.GOOS
	}
	return "nem-agent on " + h
}

// ValidBase checks a site URL: https, or http for a local dev host only. It returns the
// normalized "scheme://host[:port]".
func ValidBase(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("not a site URL: %q", raw)
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !LocalHost(u.Hostname()) {
			return "", fmt.Errorf("plain http only for a local dev site, not %s", u.Hostname())
		}
	default:
		return "", fmt.Errorf("not a site URL: %q", raw)
	}
	return u.Scheme + "://" + u.Host, nil
}

// LocalHost — a host that can only be this machine (dev sites).
func LocalHost(host string) bool {
	h := strings.ToLower(host)
	return h == "localhost" || h == "127.0.0.1" || h == "::1" || strings.HasSuffix(h, ".localhost")
}

// Account is the keychain account for a site: its host, so a dev site's key never
// overwrites the real one.
func Account(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return base
	}
	return strings.ToLower(u.Host)
}
