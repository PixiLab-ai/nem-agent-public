package peek

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCategorize(t *testing.T) {
	cases := []struct {
		name string
		f    Front
		want App
	}{
		{"vs code on X11", Front{Class: "Code", Instance: "code"}, App{Category: Coding}},
		{"a terminal", Front{Class: "kitty", Instance: "kitty"}, App{Category: Terminal}},
		{"firefox", Front{Class: "firefox", Exe: "/usr/lib/firefox/firefox"}, App{Category: Browsing}},
		{"discord on Windows", Front{Exe: `C:\Users\x\AppData\Local\Discord\app-1.0\Discord.exe`}, App{Category: Chatting}},
		{"zoom on macOS", Front{BundleID: "us.zoom.xos"}, App{Category: Meeting}},
		{"a JetBrains IDE on macOS", Front{BundleID: "com.jetbrains.goland"}, App{Category: Coding}},
		{"a native Steam game", Front{Class: "Hades", Exe: "/mnt/games/SteamLibrary/steamapps/common/Hades/Hades"}, App{Category: Gaming, Game: "Hades"}},
		{"a Proton game by its path", Front{Class: "eldenring.exe", Cmdline: `Z:\home\x\.steam\steam\steamapps\common\ELDEN RING\Game\eldenring.exe`}, App{Category: Gaming, Game: "ELDEN RING"}},
		{"a Proton game, its program Proton's", Front{Class: "eldenring.exe", Exe: "/mnt/games/SteamLibrary/steamapps/common/Proton 9.0 (Beta)/files/bin/wine64-preloader", Cmdline: `/mnt/games/SteamLibrary/steamapps/common/Proton 9.0 (Beta)/files/bin/wine64 Z:\home\x\.steam\steam\steamapps\common\ELDEN RING\Game\eldenring.exe`}, App{Category: Gaming, Game: "ELDEN RING"}},
		{"Steam's runtime alone is no game", Front{Exe: "/mnt/games/SteamLibrary/steamapps/common/SteamLinuxRuntime_sniper/pressure-vessel/libexec/steam-runtime-tools-0/srt-bwrap"}, App{Category: Other}},
		{"Proton's wineserver alone is no game", Front{Exe: "/mnt/games/SteamLibrary/steamapps/common/Proton 9.0/files/bin/wineserver", Cmdline: "/mnt/games/SteamLibrary/steamapps/common/Proton 9.0/files/bin/wineserver"}, App{Category: Other}},
		{"a Windows game in Epic", Front{Exe: `D:\Epic Games\Fortnite\FortniteGame\Binaries\Win64\FortniteClient-Win64-Shipping.exe`}, App{Category: Gaming, Game: "Fortnite"}},
		{"the app itself", Front{Class: "nemini"}, App{Category: Nemini}},
		{"something unknown", Front{Class: "frobnicator"}, App{Category: Other}},
		{"nothing in front", Front{}, App{Category: None}},
		{"a DAW named reaper", Front{Class: "REAPER"}, App{Category: Creating}},
	}
	for _, c := range cases {
		if got := Categorize(c.f); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestSteamGameName(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows: os.UserHomeDir reads this one
	apps := filepath.Join(home, ".steam", "steam", "steamapps")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	acf := "\"AppState\"\n{\n\t\"appid\"\t\t\"1245620\"\n\t\"name\"\t\t\"ELDEN RING\"\n}\n"
	if err := os.WriteFile(filepath.Join(apps, "appmanifest_1245620.acf"), []byte(acf), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Categorize(Front{Class: "steam_app_1245620"}); got != (App{Category: Gaming, Game: "ELDEN RING"}) {
		t.Fatalf("got %+v", got)
	}
	if got := Categorize(Front{Class: "steam_app_999"}); got != (App{Category: Gaming}) {
		t.Fatalf("unknown app id: %+v", got)
	}
}
