# Nemini desktop

Nemini on your computer: the Nemini web app in its own window, your companion's pet living on your desktop, and
a small background service that keeps them close while you're there.

- **The window** is the Nemini web app itself, so every feature is there and stays current with
  the web. Every model call works exactly as on the web: the same API, and the server picks your
  own provider key or Nemini's.
- **The pet** sits on top of your other windows, click-through everywhere but itself. It says
  small things, hops, takes a tap, shows when they're writing, and opens its card on a right-click.
  Drag it anywhere; it stays where you drop it.
- **Notifications** for their messages while the window is in the background.
- **Their desktop as a folder** (if you choose one in the app's settings): their Nemini desktop,
  downloaded into an empty folder on your computer and kept in step both ways. For now it's mostly
  an easier way to give them files and to read theirs; later, they'll be able to do more with it
  themselves.
- **Peek** (off by default): the pet notices what *kind* of thing you're doing — coding, gaming,
  chatting… — and teases you about it. It reads only which app is in front and for how long; see
  [SECURITY.md](SECURITY.md) for exactly what it reads and sends.

## Install

Download the latest release for your system from the
[Releases page](https://github.com/PixiLab-ai/nem-agent-public/releases/latest):

| System | File | Notes |
|---|---|---|
| Linux | `nemini-<version>-x86_64.AppImage` | make it executable (`chmod +x`), then run it; needs FUSE 2 (`libfuse2`) |
| Debian, Ubuntu | `nemini-<version>-amd64.deb` | `sudo apt install ./nemini-<version>-amd64.deb` |
| macOS | `nemini-<version>-arm64.dmg` (Apple Silicon) or `-x64.dmg` (Intel) | drag Nemini to Applications |
| Windows | `nemini-<version>-x64.exe` | run the installer |

Until the builds are signed, macOS and Windows warn the first time: on macOS, right-click the app
and choose **Open**; on Windows, **More info → Run anyway**.

### Updates

The app checks for a new release when it starts and every few hours, downloads it in the
background, and installs it when you quit — or right away with **Restart and install** in the
settings (**Restart to update** in the tray menu).

Closing the window doesn't quit: Nemini keeps running in the background with the pet and
notifications. To quit, use **Quit Nemini** in the settings (or the tray menu). The Debian package asks for your password to install an update. What changed in each
version: [CHANGELOG.md](CHANGELOG.md).

## Signing in

The app signs in with a key, and only a key: no password, no login page inside the app.

1. On the Nemini website, open **Settings → App** and create a key.
2. In the app, paste it in **Pair with a key** (it opens by itself the first time, and lives in
   the tray menu).

The key is kept in the system keychain by the background service. The window's calls to the
Nemini API carry it; the page itself never sees it, and it's only ever sent to the site it was made
on. Unpair from the tray menu, and revoke the key on the website.

A few things need a browser session and open on the website instead: keys, model providers,
webhooks, billing, and exporting or deleting your data.

## Layout

| Path | What |
|---|---|
| `apps/desktop` | the Electron shell: main window, pet overlay, pair window, tray, notifications |
| `daemon` | `nemd`, the background service (Go): the key, the chat socket, their desktop's folder sync (`desksync`), Peek (`peek`), private IPC |
| `packages/pet` | the pet: sprites, state, the Vue components the overlay renders |

`nemd` holds the key in the OS keychain and keeps their chat socket open only while you're active
at the computer. The shell talks to it over a Unix socket private to your user (a named pipe on
Windows); nothing listens on a network port.

## Developing

Needs Node 22+, pnpm 10 and Go 1.23+.

```bash
pnpm install
pnpm daemon                                   # build nemd (daemon/bin/nemd)
pnpm --filter @nem-agent/desktop start        # the app
pnpm --filter @nem-agent/desktop demo         # just the pet, no account, no network
pnpm daemon:test                              # nemd's tests
pnpm typecheck
```

`electron . --base=<url>` points the app at another Nemini site (a development server). The app's
other switches are listed at the top of `apps/desktop/src/main/main.ts`.

## Packaging

Releases are built by the `release` workflow on Linux, macOS and Windows runners when a version
tag is pushed, and uploaded to a draft release that a person checks before publishing. To build
installers locally:

```bash
pnpm --filter @nem-agent/desktop dist:linux   # AppImage + .deb in apps/desktop/out/
pnpm --filter @nem-agent/desktop dist:mac     # dmg + zip (needs a Mac and a Developer ID to sign)
pnpm --filter @nem-agent/desktop dist:win     # NSIS installer (signing needs a certificate)
```

Each build compiles `nemd` for the target and ships it next to the app. The shipped binary has its
Electron fuses set (it can't be reused as a plain Node runtime, and loads only its own app archive).

## Platforms

- **Linux:** X11 desktops natively; on Wayland the app runs through XWayland, because the pet needs
  a free position, an input shape and always-on-top, which Wayland doesn't give apps.
- **macOS and Windows:** the pet window ignores the mouse except over the pet's own pixels.

## Security

How the app keeps your key and what it sends, and how to report a vulnerability:
[SECURITY.md](SECURITY.md).

## License

To be decided before the repository opens.
