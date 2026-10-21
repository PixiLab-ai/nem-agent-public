// The pet's window: frameless, transparent, on top of everything (unless the human said otherwise in
// the settings), out of the taskbar, never focused, and click-through everywhere except the pet itself.
//
// Click-through per platform:
//   - Linux X11: the renderer reports the pet's hit boxes and the window takes an X shape of
//     just those boxes; outside them, clicks fall through to what's below. (X11 can't forward
//     mouse movement to a window that ignores the mouse, so the other way isn't available.)
//   - Windows and macOS: the whole window ignores the mouse, with movement still forwarded, and
//     the renderer turns hits back on while the pointer is over an opaque pixel of the pet.
// Dragging is done here, not by the window manager: while the button is held, the main process
// follows the cursor (a 60 Hz timer that only runs during a drag).
import { BrowserWindow, type Display, ipcMain, type Rectangle, screen } from 'electron'
import type { Point } from './settings'

export const OVERLAY_W = 260
export const OVERLAY_H = 310 // room for the pet's card above the sprite
const MARGIN = 24
const DRAG_THRESHOLD = 4

/** How this platform makes the transparent parts click-through. */
export type ClickThrough = 'shape' | 'forward'

export function clickThroughMode(): ClickThrough {
  // Wayland needs its own answer (spike A): neither a shape nor forwarding is a given there
  return process.platform === 'linux' ? 'shape' : 'forward'
}

export interface OverlayOptions {
  preload: string
  page: string
  saved?: { display: number, at: Point }
  onMoved: (display: number, at: Point) => void
  onTap: () => Promise<unknown>
  /** the shell's language, for the pet's card */
  lang: string
  /** above every window; false = a plain window, under the ones in use (settings: petOnTop) */
  onTop: boolean
}

function clampInto(area: Rectangle, x: number, y: number): Point {
  return {
    x: Math.round(Math.min(Math.max(x, area.x), area.x + area.width - OVERLAY_W)),
    y: Math.round(Math.min(Math.max(y, area.y), area.y + area.height - OVERLAY_H)),
  }
}

function defaultSpot(d: Display): Point {
  const a = d.workArea
  return { x: a.x + a.width - OVERLAY_W - MARGIN, y: a.y + a.height - OVERLAY_H - MARGIN }
}

export class Overlay {
  readonly win: BrowserWindow
  readonly mode: ClickThrough
  /** the page has loaded: what is sent before this is lost (a window made again sends after it) */
  readonly ready: Promise<void>
  /** on top / not on top switches on this window; false = Linux, where it takes a new window (see the constructor) */
  readonly restacks = process.platform !== 'linux'
  private shown = false
  private wanted = false
  private drag: { timer: NodeJS.Timeout, start: Point, win: Point, moved: boolean, guard: NodeJS.Timeout } | null = null

  constructor(private o: OverlayOptions) {
    this.mode = clickThroughMode()
    const at = this.startSpot()
    // Linux X11: Chromium makes a window that can't take focus override-redirect — the window manager never
    // stacks it, so the pet is above everything whatever its level and "not on top" meant nothing (checked with
    // xwininfo, 2026-10-21). Not on top, it is a desktop-type window there: managed, it sits on the desktop —
    // above the wallpaper, under every app (xfwm4's stacking list) — and the window manager keeps it out of the
    // taskbar and on every workspace (Electron dropped skipTaskbar on Linux, so a normal window would show in the
    // taskbar). X11 can't turn one kind into the other: the main process makes a new window when the setting
    // changes (restacks = false).
    const managed = process.platform === 'linux' && !o.onTop
    this.win = new BrowserWindow({
      width: OVERLAY_W,
      height: OVERLAY_H,
      x: at.x,
      y: at.y,
      show: false,
      frame: false,
      transparent: true,
      backgroundColor: '#00000000',
      hasShadow: false,
      resizable: false,
      minimizable: false,
      maximizable: false,
      fullscreenable: false,
      skipTaskbar: true,
      alwaysOnTop: o.onTop,
      focusable: managed,
      title: 'Nemini pet',
      ...(process.platform === 'darwin' ? { type: 'panel' } : managed ? { type: 'desktop' } : {}),
      webPreferences: {
        preload: o.preload,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        webviewTag: false,
        spellcheck: false,
      },
    })
    if (o.onTop) this.win.setAlwaysOnTop(true, 'screen-saver')
    if (process.platform === 'darwin') this.win.setVisibleOnAllWorkspaces(true, { visibleOnFullScreen: true })
    this.ready = new Promise(resolve => this.win.webContents.once('did-finish-load', () => resolve()))
    if (this.mode === 'forward') this.win.setIgnoreMouseEvents(true, { forward: true })
    else this.setShape([])

    // our own page only: no navigation, no popups
    this.win.webContents.on('will-navigate', e => e.preventDefault())
    this.win.webContents.setWindowOpenHandler(() => ({ action: 'deny' }))

    this.wireIpc()
    screen.on('display-removed', this.onDisplays)
    screen.on('display-metrics-changed', this.onDisplays)
    void this.win.loadFile(o.page, { query: { mode: this.mode, lang: o.lang } })
  }

  private onDisplays = () => this.keepOnScreen()

  private startSpot(): Point {
    const s = this.o.saved
    const d = s && screen.getAllDisplays().find(x => x.id === s.display)
    if (s && d) return clampInto(d.workArea, s.at.x, s.at.y)
    return defaultSpot(screen.getPrimaryDisplay())
  }

  private keepOnScreen() {
    if (this.win.isDestroyed()) return
    const [x, y] = this.win.getPosition()
    const d = screen.getDisplayNearestPoint({ x: x + OVERLAY_W / 2, y: y + OVERLAY_H / 2 })
    const p = clampInto(d.workArea, x, y)
    if (p.x !== x || p.y !== y) this.win.setPosition(p.x, p.y)
  }

  /** Only the pet's boxes take clicks. An empty list must not reach setShape: that would make the whole window solid. */
  private setShape(rects: Rectangle[]) {
    const clean = rects
      .map(r => ({ x: Math.max(0, Math.floor(r.x)), y: Math.max(0, Math.floor(r.y)), width: Math.ceil(r.width), height: Math.ceil(r.height) }))
      .filter(r => r.width > 0 && r.height > 0)
    this.win.setShape(clean.length ? clean : [{ x: 0, y: 0, width: 1, height: 1 }])
  }

  private fromHere(e: Electron.IpcMainEvent | Electron.IpcMainInvokeEvent) {
    return !this.win.isDestroyed() && e.sender === this.win.webContents
  }

  private wireIpc() {
    ipcMain.on('overlay:hits', (e, rects: Rectangle[]) => {
      if (!this.fromHere(e) || this.mode !== 'shape' || !Array.isArray(rects)) return
      this.setShape(rects.slice(0, 64))
    })
    ipcMain.on('overlay:hover', (e, over: boolean) => {
      if (!this.fromHere(e) || this.mode !== 'forward') return
      this.win.setIgnoreMouseEvents(!over, { forward: true })
    })
    ipcMain.handle('overlay:grab-start', (e) => {
      if (!this.fromHere(e)) return
      this.endDrag()
      const start = screen.getCursorScreenPoint()
      const [wx, wy] = this.win.getPosition()
      const timer = setInterval(() => this.follow(), 16)
      // no button-up ever arrives if the grab is lost; never follow the cursor forever
      const guard = setTimeout(() => this.endDrag(), 30_000)
      this.drag = { timer, start, win: { x: wx, y: wy }, moved: false, guard }
    })
    ipcMain.handle('overlay:grab-end', (e) => {
      if (!this.fromHere(e)) return { moved: false }
      return { moved: this.endDrag() }
    })
    ipcMain.handle('overlay:tap', async (e) => {
      if (!this.fromHere(e)) return null
      return this.o.onTap()
    })
  }

  private follow() {
    const d = this.drag
    if (!d || this.win.isDestroyed()) return
    const c = screen.getCursorScreenPoint()
    const dx = c.x - d.start.x
    const dy = c.y - d.start.y
    if (!d.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) return
    d.moved = true
    this.win.setPosition(Math.round(d.win.x + dx), Math.round(d.win.y + dy))
  }

  /** Stops a drag; returns whether the pet actually moved (a tap otherwise). */
  private endDrag(): boolean {
    const d = this.drag
    if (!d) return false
    clearInterval(d.timer)
    clearTimeout(d.guard)
    this.drag = null
    if (!d.moved || this.win.isDestroyed()) return false
    this.keepOnScreen()
    const [x, y] = this.win.getPosition()
    const display = screen.getDisplayNearestPoint({ x: x + OVERLAY_W / 2, y: y + OVERLAY_H / 2 })
    this.o.onMoved(display.id, { x, y })
    return true
  }

  /** Send the renderer something (pet info, a chat frame, a demo step). */
  send(channel: string, payload?: unknown) {
    if (!this.win.isDestroyed()) this.win.webContents.send(channel, payload)
  }

  /** Show or hide the window (hidden = no pet to show, or the human hid it on this device). */
  setVisible(on: boolean) {
    this.wanted = on
    if (this.win.isDestroyed()) return
    if (on && !this.shown) {
      this.win.showInactive()
      this.shown = true
    }
    else if (!on && this.shown) {
      this.win.hide()
      this.shown = false
    }
  }

  get visible() { return this.wanted }

  /** Above every window, or a plain one. Electron has no "always below": off, the pet stays where it is in the
   *  stack and the windows the human brings up cover it. */
  setOnTop(on: boolean) {
    if (this.win.isDestroyed() || !this.restacks) return
    if (on) this.win.setAlwaysOnTop(true, 'screen-saver')
    else this.win.setAlwaysOnTop(false)
  }

  destroy() {
    this.endDrag()
    screen.off('display-removed', this.onDisplays)
    screen.off('display-metrics-changed', this.onDisplays)
    for (const ch of ['overlay:hits', 'overlay:hover']) ipcMain.removeAllListeners(ch)
    for (const ch of ['overlay:grab-start', 'overlay:grab-end', 'overlay:tap']) ipcMain.removeHandler(ch)
    if (!this.win.isDestroyed()) this.win.destroy()
  }
}
