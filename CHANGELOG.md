# Changelog

All notable changes to the Nemini desktop app. Versions follow [semver](https://semver.org).

## Unreleased

- **The app's own settings window** (tray → Settings…, Ctrl+, — Cmd+, on macOS — or the website's
  Settings → App inside the app): the pet and start at login on this computer, signing this
  computer out, the version and a check for updates.
- **Your companion's desktop as a folder on this computer — the first step:** choose or create an
  empty folder for it in the settings (a folder with files in it, a whole drive or your home folder
  itself is refused). Syncing itself comes next.
- Development runs keep their own settings, apart from the installed app.

## 0.1.0-alpha.1 — 2026-10-19

The first preview (alpha): expect rough edges; builds are unsigned.

- **The Nemini web app in its own window**, signed in with a key made on the website (Settings →
  Keys) and pasted in the app — no password, no login page. The parts that need a browser
  session open on the website.
- **Your companion's pet on the desktop**: on top of other windows, click-through everywhere but itself. It
  speaks in small bubbles, hops, takes a tap, shows when they're writing and how their last reply
  felt, and opens its card on a right-click. Drag it anywhere; it stays where you drop it.
- **Notifications** for their messages while the window is in the background.
- **Presence**: their live connection is open only while you're active at the computer.
- **Start at login** (off by default), quietly: just the pet and the tray.
- **Updates** downloaded in the background, installed when you quit.
- **Six languages**: English, French, German, Chinese, Spanish, Japanese.
- **Linux** (AppImage, .deb; X11 and, through XWayland, Wayland), **macOS** and **Windows** builds.
