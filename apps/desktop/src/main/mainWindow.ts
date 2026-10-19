// The main window is the Nemini web app itself, so the whole feature suite is there on day one
// and stays current with every web deploy. It signs in with this computer's key only: no cookie,
// no login page. The key rides every API call the site's own pages make — added here, in the
// main process, so the page never sees it — and the web app, which knows it's in the app by the
// NeminiDesktop token in its User-Agent, asks GET /swarm/me who it is (swarm: lib/desktopApp.ts).
// Model calls work exactly as on the web: the same API, and the server picks the human's own
// provider key or Nemini's lanes.
//
// It runs locked down: sandboxed, isolated, no Node, no preload, no <webview>. The page stays on
// the Nemini origin; every new window and every way off the site opens in the human's own
// browser — that's also where the parts that need a browser session live (keys, billing, the
// danger zone). Mic and notifications are granted to the Nemini origin only.
import { present } from './inactive'
import { BrowserWindow, type Session, session, shell } from 'electron'
import { originOf, sameSite } from './origin'

/** Every Nemini API the web app calls lives under this path (swarm, the UI prefs, the memory graph). */
const API_PATH = '/_special/rest/Pixi/api/'

const ALLOWED_PERMISSIONS = new Set([
  'media', // the call (microphone)
  'notifications',
  'clipboard-sanitized-write',
  'fullscreen',
  'pointerLock',
])

export interface MainWindowOptions {
  base: string
  userAgentTag: string
  /** this computer's key, or null while it isn't paired */
  key: () => string | null
  /** development: log outside links instead of opening them (`--no-external`) */
  noExternal?: boolean
  /** the site's links into the app itself (`nemini-app://settings`), Ctrl+, and Ctrl+Q: what to do */
  onAppLink?: (what: string) => void
}

/** The chat socket is ws(s)://; for "is it the site" it's the same host as http(s)://. */
function asHttp(url: string): string {
  return url.replace(/^ws(s?):/i, 'http$1:')
}

/**
 * Who asked for this request: only the site's own pages get the key. Never an iframe from
 * elsewhere — a third-party embed must not be able to open her chat as you, which today's cookie
 * rules (SameSite) already prevent on the web.
 */
function fromTheSite(d: Electron.OnBeforeSendHeadersListenerDetails, base: string): boolean {
  if (d.initiatorOrigin) return sameSite(d.initiatorOrigin, base)
  try {
    if (d.frame && !d.frame.isDestroyed()) return sameSite(d.frame.origin, base)
  }
  catch { /* the frame went away mid-request */ }
  const origin = d.requestHeaders.Origin || d.requestHeaders.origin
  return !!origin && sameSite(origin, base)
}

/** The persistent profile of the app's window: no login lives here, only the site's own storage. */
export function siteSession(o: MainWindowOptions): Session {
  const ses = session.fromPartition('persist:nemini')
  // The site (and the server) tell the app from a browser by this token (via "app", app mode).
  if (!ses.getUserAgent().includes(o.userAgentTag)) ses.setUserAgent(`${ses.getUserAgent()} ${o.userAgentTag}`)
  ses.setPermissionRequestHandler((_wc, permission, callback, details) => {
    callback(ALLOWED_PERMISSIONS.has(permission) && sameSite(details.requestingUrl, o.base))
  })
  ses.setPermissionCheckHandler((_wc, permission, requestingOrigin) => {
    return ALLOWED_PERMISSIONS.has(permission) && sameSite(requestingOrigin, o.base)
  })
  // the key, on the site's API calls only (sockets included), from the site's pages only
  ses.webRequest.onBeforeSendHeaders((d, callback) => {
    const key = o.key()
    if (key) {
      const url = asHttp(d.url)
      let path = ''
      try { path = new URL(url).pathname }
      catch { /* not a URL we sign */ }
      if (sameSite(url, o.base) && path.startsWith(API_PATH) && fromTheSite(d, o.base)) {
        d.requestHeaders.Authorization = `Bearer ${key}`
      }
    }
    callback({ requestHeaders: d.requestHeaders })
  })
  return ses
}

export function createMainWindow(o: MainWindowOptions): BrowserWindow {
  const ses = siteSession(o)
  const win = new BrowserWindow({
    width: 1180,
    height: 820,
    minWidth: 380,
    minHeight: 520,
    show: false,
    title: 'Nemini',
    backgroundColor: '#1a1626',
    autoHideMenuBar: true,
    webPreferences: {
      session: ses,
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
      webviewTag: false,
      spellcheck: true,
    },
  })
  win.once('ready-to-show', () => present(win))

  // The page stays on the Nemini site; when the page itself would leave it (outside links, Stripe
  // checkout), the human's own browser opens instead. Only the page: iframes (captchas, payment
  // fields) load and redirect inside themselves as they do on the web — steering those breaks
  // them. Every new window opens in the browser too, the site's own included: that's how the web
  // app sends the human to the parts that need a browser session (openOnWebsite).
  const external = (url: string, how: string) => {
    if (!/^https?:\/\//i.test(url) && !/^mailto:/i.test(url)) return
    let where = 'mailto'
    try { const u = new URL(url); where = u.origin + u.pathname }
    catch { /* mailto: or malformed */ }
    if (o.noExternal) {
      console.log(`[nem] outside the window (${how}), not opened (--no-external): ${where}`)
      return
    }
    console.log(`[nem] outside the window (${how}), opening in the browser: ${originOf(url) || 'mailto'}`)
    void shell.openExternal(url)
  }
  // The site's own links into the app (its Settings › App page, in the app: nemini-app://settings)
  // never navigate: the app opens what they name.
  const appLink = (url: string): boolean => {
    const m = /^nemini-app:\/*([a-z-]+)/i.exec(url)
    if (!m) return false
    o.onAppLink?.(m[1].toLowerCase())
    return true
  }
  win.webContents.on('will-navigate', (e) => {
    if (appLink(e.url)) { e.preventDefault(); return }
    if (e.isMainFrame && !sameSite(e.url, o.base)) { e.preventDefault(); external(e.url, 'navigate') }
  })
  // Ctrl+, (Cmd+, on macOS): the app's settings, as in other desktop apps
  win.webContents.on('before-input-event', (e, input) => {
    if (input.type === 'keyDown' && input.key === ',' && (process.platform === 'darwin' ? input.meta : input.control) && !input.alt && !input.shift) {
      e.preventDefault()
      o.onAppLink?.('settings')
    }
    // Ctrl+Q quits (Linux, Windows; macOS has ⌘Q in its app menu)
    if (input.type === 'keyDown' && process.platform !== 'darwin' && input.control && !input.alt && !input.shift && input.key.toLowerCase() === 'q') {
      e.preventDefault()
      o.onAppLink?.('quit')
    }
  })
  win.webContents.on('will-redirect', (e) => {
    if (e.isMainFrame && !sameSite(e.url, o.base)) { e.preventDefault(); external(e.url, 'redirect') }
  })
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (!appLink(url)) external(url, 'new window')
    return { action: 'deny' }
  })
  win.webContents.on('will-attach-webview', e => e.preventDefault())

  void win.loadURL(`${o.base}/me`)
  return win
}
