package desksync

import (
	"path"
	"strings"
)

// What never goes up from the folder: our own files, the file managers' notes, editors' and
// office suites' temp and lock files, downloads still arriving, build caches, and secrets.
// A save that writes a temp file then renames it over the real one leaves only the real one.

var ignoredNames = map[string]bool{
	MarkerName: true, ".DS_Store": true, "Thumbs.db": true, "desktop.ini": true, ".directory": true,
	".localized": true, "Icon\r": true, "$RECYCLE.BIN": true, "4913": true,
	".git": true, ".svn": true, ".hg": true, "node_modules": true, ".venv": true, "__pycache__": true,
	".env": true,
}

var ignoredPrefixes = []string{tmpPrefix, "._", ".#", "~$", ".~lock.", ".goutputstream-", ".Trash-", "id_rsa", "id_ed25519", "id_ecdsa", ".env."}

var ignoredSuffixes = []string{"~", ".swp", ".swo", ".swx", ".tmp", ".crdownload", ".part", ".partial", ".download",
	"___jb_tmp___", "___jb_old___", ".pem", ".key", ".p12", ".pfx"}

// ignored — a path (server form, "/a/b") the folder never sends up: any segment of it ignored.
func ignored(p string) bool {
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if ignoredName(seg) {
			return true
		}
	}
	return false
}

func ignoredName(name string) bool {
	if name == "" || ignoredNames[name] {
		return true
	}
	if strings.HasPrefix(name, "#") && strings.HasSuffix(name, "#") && len(name) > 1 {
		return true // emacs' autosave
	}
	for _, p := range ignoredPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, s := range ignoredSuffixes {
		if strings.HasSuffix(name, s) {
			return true
		}
	}
	return false
}

// protectedPaths are her system files (the server keeps the same list): they come down like any
// file, but a change made to them in the folder is never sent up — her version comes back.
var protectedPaths = map[string]bool{
	"/personality.md": true, "/avatar.png": true, "/lessons.md": true, "/privacy.md": true, "/avatar.json": true,
	"/avatar-idle.mp4": true,
}

func isProtected(p string) bool {
	if protectedPaths[p] {
		return true
	}
	// her faces: /avatar-<pose>.png
	return path.Dir(p) == "/" && strings.HasPrefix(path.Base(p), "avatar-") && strings.HasSuffix(p, ".png")
}
