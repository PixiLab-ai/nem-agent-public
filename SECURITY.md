# Security

## Reporting a vulnerability

Please report it privately through GitHub's private vulnerability reporting: the repository's
**Security** tab → **Report a vulnerability**. Please don't open a public issue. We answer there.

Only the latest release is supported; fixes ship as new releases, which installed apps pick up on
their own.

## How the app handles your account

- **Your key.** You create it on the Nemini website (Settings → App) and paste it into the app
  once. The background service (`nemd`) keeps it in the system keychain — Keychain on macOS,
  Credential Manager on Windows, the Secret Service on Linux — never in a settings file.
- **The window** is the Nemini web app, sandboxed and isolated from the rest of the app. The app
  adds the key to the window's calls to the Nemini API, only for requests from Nemini's own pages,
  and only towards the site the key was made for. The page's scripts never see the key.
- **What the key can't do.** Creating or revoking keys, changing model providers, webhooks,
  billing, and exporting or deleting your data need a browser session on the website. A stolen
  key can't lock you out or take over those.
- **No open ports.** The app and `nemd` talk over a channel only your user account can open (a
  Unix socket, a named pipe on Windows). Nothing listens on the network.
- **Presence.** Her live connection is open only while you're active at the computer — not while
  it's idle, locked or asleep.
- **What the server records about a key**: when it was last used, from which IP address, and its
  User-Agent (the app's version and your operating system). You see it in Settings → App.
- **Revoking.** Remove the key in Settings → App on the website: its connections close at once,
  and the app shows itself unpaired. Unpairing in the app forgets the key on that computer.
- **The installed app** can't be reused as a general-purpose Node.js runtime (Electron fuses), loads
  only its own code, and has no developer tools.
