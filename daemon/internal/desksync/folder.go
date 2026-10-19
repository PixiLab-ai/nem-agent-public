// Package desksync keeps a companion's desk (her files on the site) and a folder on this computer
// in step: the human picks or creates an empty folder, her files come down into it, and what the
// human changes there goes up to her desk.
//
// This file is the folder itself: which folders may hold her desk, and the marker that says a
// folder is hers.
package desksync

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MarkerName is the file at the root of a synced folder: which companion and which site it holds.
// Local only, never sent up; it lets a folder be re-linked after a reinstall, and tells a missing
// or unmounted folder apart from one whose files were all deleted.
const MarkerName = ".nemini-sync"

// Marker is the marker file's content (never the key).
type Marker struct {
	Companion string    `json:"companion"`
	Site      string    `json:"site"`
	Created   time.Time `json:"created"`
}

// FolderError is a refused folder: Code is what the shell shows in words.
type FolderError struct {
	Code string // not_absolute · not_found · not_a_folder · too_broad · not_empty · other_companion · not_writable
	Path string
}

func (e *FolderError) Error() string { return fmt.Sprintf("folder %s: %s", e.Path, e.Code) }

// junk is what an empty folder may already hold: the file managers' own notes.
var junk = map[string]bool{".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, ".directory": true, ".localized": true}

// Check says whether root may hold this companion's desk: an absolute path to a folder that is
// empty (but for file-manager junk) or already marked for this companion, and not a place too
// broad to fill with her files (a drive's root, the home folder itself).
func Check(root, companion string) error {
	if !filepath.IsAbs(root) {
		return &FolderError{"not_absolute", root}
	}
	root = filepath.Clean(root)
	if tooBroad(root) {
		return &FolderError{"too_broad", root}
	}
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return &FolderError{"not_found", root}
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return &FolderError{"not_a_folder", root}
	}
	if m, err := ReadMarker(root); err == nil {
		if m.Companion != companion {
			return &FolderError{"other_companion", root}
		}
		return nil // hers already: re-linked as it is
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !junk[e.Name()] {
			return &FolderError{"not_empty", root}
		}
	}
	return nil
}

// tooBroad — a filesystem root or the human's home folder itself.
func tooBroad(root string) bool {
	if filepath.Dir(root) == root { // "/", "C:\", "\\server\share\"
		return true
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.Clean(home) == root {
		return true
	}
	return false
}

// Claim checks root and marks it as this companion's folder.
func Claim(root, companion, site string) error {
	if err := Check(root, companion); err != nil {
		return err
	}
	root = filepath.Clean(root)
	if m, err := ReadMarker(root); err == nil && m.Companion == companion {
		return nil
	}
	raw, err := json.MarshalIndent(Marker{Companion: companion, Site: site, Created: time.Now().UTC()}, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(root, MarkerName+".tmp")
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o644); err != nil {
		if os.IsPermission(err) {
			return &FolderError{"not_writable", root}
		}
		return err
	}
	return os.Rename(tmp, filepath.Join(root, MarkerName))
}

// ReadMarker reads root's marker.
func ReadMarker(root string) (*Marker, error) {
	raw, err := os.ReadFile(filepath.Join(root, MarkerName))
	if err != nil {
		return nil, err
	}
	var m Marker
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	if strings.TrimSpace(m.Companion) == "" {
		return nil, errors.New("marker without a companion")
	}
	return &m, nil
}
