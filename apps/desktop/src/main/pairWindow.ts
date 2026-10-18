// "Pair with a key": the one way this computer signs in. The human makes a key on the website
// (Settings → App), pastes it here; nemd checks it against the site before keeping it in the OS
// keychain, and the app's window is signed in with it from then on. No login, no cookie, no
// OAuth inside the app.
import { BrowserWindow, ipcMain, shell } from 'electron'
import type { Lang, T } from '../shared/i18n'

export interface PairWindowOptions {
  preload: string
  page: string
  base: string
  /** checks and stores the key; resolves with an error code the page can explain */
  pair: (token: string) => Promise<{ ok: true } | { ok: false, error: string }>
  onPaired: () => void
  lang: Lang
  t: T
}

let current: BrowserWindow | null = null

export function openPairWindow(o: PairWindowOptions): BrowserWindow {
  if (current && !current.isDestroyed()) {
    current.show()
    current.focus()
    return current
  }
  const win = new BrowserWindow({
    width: 460,
    height: 400,
    resizable: false,
    minimizable: false,
    maximizable: false,
    fullscreenable: false,
    title: o.t('pair.title'),
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
  win.once('ready-to-show', () => win.show())
  win.webContents.on('will-navigate', e => e.preventDefault())
  win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))

  const mine = (e: Electron.IpcMainInvokeEvent | Electron.IpcMainEvent) => !win.isDestroyed() && e.sender === win.webContents
  const onSubmit = async (e: Electron.IpcMainInvokeEvent, token: unknown) => {
    if (!mine(e)) return { ok: false, error: 'not_allowed' }
    const t = typeof token === 'string' ? token.trim() : ''
    if (!t.startsWith('swm_')) return { ok: false, error: 'not_a_key' }
    const r = await o.pair(t)
    if (r.ok) {
      o.onPaired()
      setTimeout(() => { if (!win.isDestroyed()) win.close() }, 900)
    }
    return r
  }
  const onKeysPage = (e: Electron.IpcMainEvent) => {
    if (mine(e)) void shell.openExternal(`${o.base}/me/settings/app`)
  }
  ipcMain.handle('pair:submit', onSubmit)
  ipcMain.on('pair:keys-page', onKeysPage)
  win.on('closed', () => {
    ipcMain.removeHandler('pair:submit')
    ipcMain.removeListener('pair:keys-page', onKeysPage)
    if (current === win) current = null
  })

  let host = o.base
  try { host = new URL(o.base).host }
  catch { /* shown as given */ }
  void win.loadFile(o.page, { query: { site: host, lang: o.lang } })
  return win
}
