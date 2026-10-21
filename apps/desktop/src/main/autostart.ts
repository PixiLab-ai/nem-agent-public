// Start at login (device-local, off by default; the tray's "Start at login"). At login the app
// starts quietly with `--background`: the pet and the tray, no window.
//   - macOS / Windows: the OS login items (`app.setLoginItemSettings`);
//   - Linux: an XDG autostart entry, ~/.config/autostart/nemini.desktop (Electron has no
//     login-item API there).
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { app } from 'electron'

export const BACKGROUND_FLAG = '--background'

function linuxEntry(): string {
  const base = process.env.XDG_CONFIG_HOME || join(homedir(), '.config')
  return join(base, 'autostart', 'nemini.desktop')
}

/** What starts the app: the AppImage, the installed binary, or (in development) Electron + this app's folder. */
function command(): string[] {
  if (process.env.APPIMAGE) return [process.env.APPIMAGE]
  if (app.isPackaged) return [process.execPath]
  return [process.execPath, app.getAppPath()]
}

function quote(arg: string): string {
  return /^[\w@%+=:,./-]+$/.test(arg) ? arg : `"${arg.replace(/(["`$\\])/g, '\\$1')}"`
}

/** What the login item runs with: `--background` (and this app's folder in development). */
function loginArgs(): string[] {
  return app.isPackaged ? [BACKGROUND_FLAG] : [app.getAppPath(), BACKGROUND_FLAG]
}

export function autostartEnabled(): boolean {
  if (process.platform === 'linux') return existsSync(linuxEntry())
  // Windows matches the Run entry against a command line: asked without the args it was set with,
  // it answers "off" — the box ticked itself straight back off and "Start at login" could never be
  // turned on (a tester, 2026-10-21). The args are ignored on macOS.
  return app.getLoginItemSettings({ args: loginArgs() }).openAtLogin
}

export function setAutostart(on: boolean) {
  if (process.platform === 'linux') {
    const file = linuxEntry()
    if (!on) {
      rmSync(file, { force: true })
      return
    }
    mkdirSync(join(file, '..'), { recursive: true })
    const exec = [...command(), BACKGROUND_FLAG].map(quote).join(' ')
    writeFileSync(file, [
      '[Desktop Entry]',
      'Type=Application',
      'Name=Nemini',
      'Comment=Her pet on your desktop',
      `Exec=${exec}`,
      'X-GNOME-Autostart-enabled=true',
      'NoDisplay=true',
      '',
    ].join('\n'))
    return
  }
  app.setLoginItemSettings({ openAtLogin: on, args: loginArgs() })
}
