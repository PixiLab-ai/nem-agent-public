# Changelog

All notable changes to the Nemini desktop app. Versions follow [semver](https://semver.org).

## 0.1.0-alpha.2 — 2026-10-19

- **The app's own settings window** (tray → Settings…, Ctrl+, — Cmd+, on macOS — the gear in the
  website's sidebar, or its Settings → App, inside the app): their desktop's folder, Peek, the pet
  and start at login on this computer, signing this computer out, the version and a check for
  updates.
- **Your companion's desktop as a folder on this computer:** choose or create an empty folder in
  the settings, and the app downloads their desktop into it, then keeps both in step. For now it's
  mostly an easier way to give them files and to read theirs; later, they'll be able to do more
  with it themselves. What changes on their side shows up in the folder within seconds while
  you're at the computer; what you add, edit, rename or delete in the folder goes to their
  desktop. Their own files (personality, avatar…) come down but stay theirs: a change made to one
  in the folder is undone. When both sides changed the same file, both versions are kept. Many
  files deleted at once wait for your yes; files they delete land in a local trash first; when
  their desktop is full, the folder waits for room. **Sync now** in the settings doesn't wait for
  the next round.
- **Peek** (off by default, in the settings): the pet notices what *kind* of thing you're doing —
  coding, gaming, chatting, watching… and for how long — and teases you about it in its own voice:
  two hours of code, still gaming at 2 am, back in the chat for the tenth time this hour, a report
  card on Sunday or whenever you ask ("Judge me"). The app reads only which app is in front and how
  long it stays there — never a window's title, a web address, a file, the screen or what you
  type. The pet hears the kind of thing and the minutes; a game's name stays on this computer. Two
  small eyes sit on the pet while it's on; the tray pauses it for an hour, the settings forget its
  stats. It keeps quiet in meetings and over anything full screen.
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
