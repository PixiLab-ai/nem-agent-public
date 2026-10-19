// Package peek is the pet noticing what the human is up to — with the human's yes, off by
// default, on this computer: only which app is in front and for how long, turned here into a
// category (coding, gaming, chatting…). Never a window title, a web address, a file name, the
// screen, the keyboard or the clipboard; the app's own name never leaves this package except a
// game's, to the pet's own bubble.
package peek

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Category is a kind of thing the human is doing.
type Category string

const (
	Coding   Category = "coding"
	Terminal Category = "terminal"
	Browsing Category = "browsing"
	Chatting Category = "chatting"
	Meeting  Category = "meeting"
	Gaming   Category = "gaming"
	Music    Category = "music"
	Video    Category = "video"
	Creating Category = "creating"
	Writing  Category = "writing"
	Mail     Category = "mail"
	Nemini   Category = "nemini" // talking to her in the app itself
	Other    Category = "other"
	None     Category = "" // nothing in front (the desktop), or nothing known
)

//go:embed apps.json
var appsJSON []byte

// table: lower-case app key → category (apps.json)
var table = func() map[string]Category {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(appsJSON, &raw); err != nil {
		panic("peek: apps.json: " + err.Error())
	}
	t := map[string]Category{}
	for cat, list := range raw {
		if cat == "_" {
			continue
		}
		var keys []string
		if err := json.Unmarshal(list, &keys); err != nil {
			panic("peek: apps.json " + cat + ": " + err.Error())
		}
		for _, k := range keys {
			t[strings.ToLower(k)] = Category(cat)
		}
	}
	return t
}()

// Front is what the computer says about the app in front — never its title. Exe and Cmdline are
// read only to tell a game (where it's installed) and are never kept or sent.
type Front struct {
	Class    string // X11 WM_CLASS class
	Instance string // X11 WM_CLASS instance
	Exe      string // the program's path
	Cmdline  string // Linux: a Proton game's Windows path lives here
	BundleID string // macOS
	Quiet    bool   // the system says not now: full screen, a presentation (counted, never commented)
}

// App is what Peek makes of it: a category, and a game's name when it's one (the pet may say it;
// it is never stored or sent).
type App struct {
	Category Category
	Game     string
	Quiet    bool // Front.Quiet
}

var steamAppClass = regexp.MustCompile(`^steam_app_(\d+)$`)

// gameDirs: where launchers install games (a path containing one is a game)
var gameDirs = []string{"steamapps/common/", "/lutris/", "/heroic/", "/epic games/", "/gog games/", "gog galaxy/games/", "/riot games/", "/battle.net/", "/itch/apps/"}

// Categorize turns what the computer said into a category.
func Categorize(f Front) App {
	// a game: Proton's window class, or installed where games go (a Proton game's program is
	// Proton's own: its path is in the command line)
	if m := steamAppClass.FindStringSubmatch(strings.ToLower(f.Class)); m != nil {
		return App{Category: Gaming, Game: steamGameName(m[1])}
	}
	for _, raw := range []string{f.Exe, f.Cmdline} {
		if game, ok := gameIn(raw); ok {
			return App{Category: Gaming, Game: game}
		}
	}
	keys := []string{f.Class, f.Instance, programName(f.Exe), f.BundleID}
	for _, k := range keys {
		if c, ok := table[strings.ToLower(strings.TrimSpace(k))]; ok && k != "" {
			return App{Category: c}
		}
	}
	if b := strings.ToLower(f.BundleID); strings.HasPrefix(b, "com.jetbrains.") {
		return App{Category: Coding}
	}
	if f.Class == "" && f.Instance == "" && f.Exe == "" && f.BundleID == "" {
		return App{Category: None}
	}
	return App{Category: Other}
}

// programName is a path's program: its last part, lower-case, without .exe.
func programName(p string) string {
	if p == "" {
		return ""
	}
	base := filepath.Base(strings.ReplaceAll(p, `\`, "/"))
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	return strings.TrimSuffix(strings.ToLower(base), ".exe")
}

// steamTools: what Steam installs next to the games (its runtimes, Proton…): not games
var steamTools = regexp.MustCompile(`(?i)^(steamlinuxruntime|proton|steamworks|steam controller|steamvr)`)

// gameIn finds a game in a program's path or command line: the folder right after a launcher's
// (steamapps/common/ELDEN RING/… → "ELDEN RING", as written on disk; "" when too odd to say).
// Every place counts — Proton's own folder comes first in a Proton game's command line.
func gameIn(raw string) (string, bool) {
	p := strings.ReplaceAll(raw, `\`, "/")
	low := strings.ToLower(p)
	for _, d := range gameDirs {
		for off := 0; ; {
			i := strings.Index(low[off:], d)
			if i < 0 {
				break
			}
			off += i + len(d)
			name := p[off:]
			if j := strings.IndexByte(name, '/'); j >= 0 {
				name = name[:j]
			}
			name = strings.TrimSpace(name)
			if steamTools.MatchString(name) {
				continue
			}
			if len(name) > 80 {
				name = ""
			}
			return name, true
		}
	}
	return "", false
}

// steamGameName looks a Steam game's name up in Steam's own manifest on this computer ("" when
// it can't).
func steamGameName(appID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	libs := []string{filepath.Join(home, ".steam", "steam"), filepath.Join(home, ".local", "share", "Steam")}
	for _, lib := range append([]string(nil), libs...) {
		libs = append(libs, steamLibraries(filepath.Join(lib, "steamapps", "libraryfolders.vdf"))...)
	}
	for _, lib := range libs {
		if n := acfName(filepath.Join(lib, "steamapps", "appmanifest_"+appID+".acf")); n != "" {
			return n
		}
	}
	return ""
}

var vdfPath = regexp.MustCompile(`"path"\s+"([^"]+)"`)
var acfField = regexp.MustCompile(`^\s*"name"\s+"([^"]+)"`)

func steamLibraries(vdf string) []string {
	raw, err := os.ReadFile(vdf)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range vdfPath.FindAllStringSubmatch(string(raw), -1) {
		out = append(out, strings.ReplaceAll(m[1], `\\`, `\`))
	}
	return out
}

func acfName(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if m := acfField.FindStringSubmatch(sc.Text()); m != nil && len(m[1]) <= 80 {
			return m[1]
		}
	}
	return ""
}
