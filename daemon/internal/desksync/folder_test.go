package desksync

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func code(err error) string {
	var fe *FolderError
	if errors.As(err, &fe) {
		return fe.Code
	}
	if err != nil {
		return "other: " + err.Error()
	}
	return ""
}

// Which folders may hold her desk: the human chooses or creates an empty one.
func TestCheck(t *testing.T) {
	base := t.TempDir()
	mk := func(name string, files ...string) string {
		d := filepath.Join(base, name)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			if err := os.WriteFile(filepath.Join(d, f), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return d
	}
	empty := mk("empty")
	junkOnly := mk("junk", ".DS_Store", "Thumbs.db")
	full := mk("full", "notes.md")
	hers := mk("hers", "notes.md")
	if err := Claim(mk("claimed"), "c-1", "https://nemini.ai"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hers, MarkerName), []byte(`{"companion":"c-1","site":"https://nemini.ai"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	other := mk("other", "a.md")
	if err := os.WriteFile(filepath.Join(other, MarkerName), []byte(`{"companion":"c-2"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(full, "notes.md")
	home, _ := os.UserHomeDir()

	cases := []struct {
		name, root, want string
	}{
		{"an empty folder", empty, ""},
		{"only file-manager junk", junkOnly, ""},
		{"a folder with files", full, "not_empty"},
		{"her own folder again (re-link)", hers, ""},
		{"a folder just claimed", filepath.Join(base, "claimed"), ""},
		{"another companion's folder", other, "other_companion"},
		{"a file", file, "not_a_folder"},
		{"a missing folder", filepath.Join(base, "nope"), "not_found"},
		{"a relative path", "relative/dir", "not_absolute"},
		{"the filesystem root", string(filepath.Separator), "too_broad"},
		{"the home folder itself", home, "too_broad"},
	}
	for _, c := range cases {
		if got := code(Check(c.root, "c-1")); got != c.want {
			t.Errorf("%s: Check(%q) = %q, want %q", c.name, c.root, got, c.want)
		}
	}
}

func TestClaimWritesTheMarker(t *testing.T) {
	d := t.TempDir()
	if err := Claim(d, "c-1", "https://nemini.ai"); err != nil {
		t.Fatal(err)
	}
	m, err := ReadMarker(d)
	if err != nil || m.Companion != "c-1" || m.Site != "https://nemini.ai" || m.Created.IsZero() {
		t.Fatalf("marker: %+v, %v", m, err)
	}
	// a second claim by the same companion is fine and keeps the first marker
	if err := Claim(d, "c-1", "https://nemini.ai"); err != nil {
		t.Fatal(err)
	}
	// another companion is refused now
	if got := code(Claim(d, "c-2", "https://nemini.ai")); got != "other_companion" {
		t.Fatalf("another companion: %q", got)
	}
}
