// The app's own settings: what only the app has, never the account's (those stay on the website).
// Her desktop folder (package desksync in nemd), the pet and start-at-login on this computer,
// signing this computer out, the version and its updates. Opened from the tray, Ctrl+, (Cmd+, on
// macOS) in the main window, and the website's own link into the app (nemini-app://settings).
import { inactive, present } from './inactive'
import { BrowserWindow, ipcMain } from 'electron'
import type { Lang, T } from '../shared/i18n'

export interface PrefsState {
  version: string
  /** the site this computer signs in to (its host) */
  site: string
  paired: boolean
  /** the companion the app shows ('' = none chosen) */
  companion: string
  pet: boolean
  autostart: boolean
  autostartAvailable: boolean
  /** her desktop's folder on this computer, whose desktop it holds, and what the sync is doing */
  desk: { root: string, name: string, sync: { state: string, done?: number, total?: number, detail?: string, count?: number } | null } | null
  /** a downloaded version waiting for a restart */
  update: string
  /** packaged builds update themselves; development runs don't */
  updatable: boolean
}

export type UpdateCheck = 'available' | 'none' | 'unavailable' | 'error'

export interface PrefsActions {
  state: () => PrefsState
  setPet: (on: boolean) => void
  /** an error message when the OS refused */
  setAutostart: (on: boolean) => string | null
  chooseFolder: (win: BrowserWindow) => Promise<{ ok: boolean, error?: string }>
  openFolder: () => void
  syncNow: () => void
  stopFolder: (win: BrowserWindow) => Promise<{ ok: boolean, error?: string }>
  checkUpdates: () => Promise<UpdateCheck>
  installUpdate: () => void
  openSource: () => void
  signOut: () => void
  pair: () => void
}

export interface PrefsWindowOptions {
  preload: string
  page: string
  lang: Lang
  t: T
  act: PrefsActions
}

let current: BrowserWindow | null = null

/** the open window gets the new state (nothing happens when it's closed) */
export function pushPrefs(state: PrefsState) {
  if (current && !current.isDestroyed()) current.webContents.send('prefs:changed', state)
}

export function openPrefsWindow(o: PrefsWindowOptions): BrowserWindow {
  if (current && !current.isDestroyed()) {
    present(current)
    if (!inactive) current.focus()
    return current
  }
  const win = new BrowserWindow({
    width: 520,
    height: 740,
    minWidth: 420,
    minHeight: 480,
    minimizable: false,
    maximizable: false,
    fullscreenable: false,
    title: o.t('prefs.title'),
    backgroundColor: '#1a1626',
    autoHideMenuBar: true,
    show: false,
    webPreferences: {
      preload: o.preload,
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: false,
      spellcheck: false,
    },
  })
  current = win
  win.once('ready-to-show', () => present(win))
  win.webContents.on('will-navigate', e => e.preventDefault())
  win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))

  // every call answers this window only
  const mine = (e: Electron.IpcMainInvokeEvent | Electron.IpcMainEvent) => !win.isDestroyed() && e.sender === win.webContents
  const handlers: Record<string, (e: Electron.IpcMainInvokeEvent, arg: unknown) => unknown> = {
    'prefs:state': () => o.act.state(),
    'prefs:pet': (_e, on) => { o.act.setPet(on === true); return o.act.state() },
    'prefs:autostart': (_e, on) => ({ error: o.act.setAutostart(on === true), state: o.act.state() }),
    'prefs:desk-choose': () => o.act.chooseFolder(win),
    'prefs:desk-stop': () => o.act.stopFolder(win),
    'prefs:updates': () => o.act.checkUpdates(),
  }
  for (const [channel, fn] of Object.entries(handlers)) {
    ipcMain.handle(channel, (e, arg) => (mine(e) ? fn(e, arg) : null))
  }
  const sends: Record<string, () => void> = {
    'prefs:desk-open': () => o.act.openFolder(),
    'prefs:desk-now': () => o.act.syncNow(),
    'prefs:install': () => o.act.installUpdate(),
    'prefs:source': () => o.act.openSource(),
    'prefs:signout': () => o.act.signOut(),
    'prefs:pair': () => o.act.pair(),
  }
  const listeners = Object.entries(sends).map(([channel, fn]) => {
    const l = (e: Electron.IpcMainEvent) => { if (mine(e)) fn() }
    ipcMain.on(channel, l)
    return [channel, l] as const
  })
  win.on('closed', () => {
    for (const channel of Object.keys(handlers)) ipcMain.removeHandler(channel)
    for (const [channel, l] of listeners) ipcMain.removeListener(channel, l)
    if (current === win) current = null
  })

  void win.loadFile(o.page, { query: { lang: o.lang } })
  return win
}
