package desksync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Entry is what the folder and her desk last agreed on for one path: the merge base. A file whose
// local bytes still match LocalSHA and whose server row still has SHA is unchanged on both sides.
type Entry struct {
	UUID      string          `json:"uuid"`
	Kind      string          `json:"kind"` // text · blob · dir
	Rev       int             `json:"rev"`
	SHA       string          `json:"sha"`       // the server's (a by-reference blob's is its URL's)
	LocalSHA  string          `json:"local_sha"` // the local bytes' when they agreed
	Size      int64           `json:"size"`      // the local size then
	MTime     int64           `json:"mtime"`     // the local mtime then (UnixNano): the quick "unchanged"
	Meta      json.RawMessage `json:"meta,omitempty"`
	URL       string          `json:"url,omitempty"` // a blob's, to fetch it again (her version back)
	Mime      string          `json:"mime,omitempty"`
	Protected bool            `json:"protected,omitempty"`
	Locked    bool            `json:"locked,omitempty"`
	// Missing: the server has it, the folder doesn't, and why (a failed or too large download):
	// never read as a local delete
	Missing string `json:"missing,omitempty"`
}

// State is the folder's agreed state with her desk, kept as JSON in nemd's config directory
// (never inside the folder).
type State struct {
	Version   int               `json:"version"`
	Companion string            `json:"companion"`
	Root      string            `json:"root"`
	Cursor    int64             `json:"cursor"` // the feed's last seq applied
	Ready     bool              `json:"ready"`  // the first download is done
	Entries   map[string]*Entry `json:"entries"`
	// LocalOnly: files the folder keeps but the desk refused (too big, a name the desk can't
	// take): not tried again until they change (path → the size + mtime refused)
	LocalOnly map[string]string `json:"local_only,omitempty"`
	// RoomWait: files her full desk refused (413 quota_exceeded): they go up once it has room again
	// (path → the size + mtime refused)
	RoomWait map[string]string `json:"room_wait,omitempty"`

	file string
}

const stateVersion = 1

// LoadState reads the state for this companion and folder; another folder or companion (or
// none yet) starts fresh.
func LoadState(file, companion, root string) (*State, error) {
	fresh := &State{Version: stateVersion, Companion: companion, Root: root, Entries: map[string]*Entry{}, LocalOnly: map[string]string{}, RoomWait: map[string]string{}, file: file}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return fresh, nil
	}
	if err != nil {
		return nil, err
	}
	var s State
	if err := json.Unmarshal(raw, &s); err != nil || s.Version != stateVersion || s.Companion != companion || s.Root != root {
		return fresh, nil
	}
	if s.Entries == nil {
		s.Entries = map[string]*Entry{}
	}
	if s.LocalOnly == nil {
		s.LocalOnly = map[string]string{}
	}
	if s.RoomWait == nil {
		s.RoomWait = map[string]string{}
	}
	s.file = file
	return &s, nil
}

// Save writes the state atomically, readable by this user only.
func (s *State) Save() error {
	if s.file == "" {
		return nil
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

// byUUID maps each known uuid to its path (a move keeps the uuid).
func (s *State) byUUID() map[string]string {
	m := make(map[string]string, len(s.Entries))
	for p, e := range s.Entries {
		if e.UUID != "" {
			m[e.UUID] = p
		}
	}
	return m
}

// PruneTrash empties the local trash of what has sat there longer than maxAge (her deletes are
// kept 30 days, like the tombstones on her desk).
func PruneTrash(dir string, maxAge time.Duration) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cut := time.Now().Add(-maxAge)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if t, err := time.ParseInLocation("20060102-150405", e.Name(), time.Local); err == nil && t.Before(cut) {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
