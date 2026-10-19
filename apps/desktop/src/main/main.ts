// Nemini on the desktop: the web app in its own window, her pet on the screen, nemd underneath.
// The app signs in with a key only (made on the website, Settings → App; pasted in the pair
// window): nemd keeps it in the OS keychain and uses it for the pet and her socket, and the main
// window's API calls carry it too. No login, no cookie, no OAuth inside the app.
//
//   electron .                      the app (nemd is started if it isn't running)
//   electron . --demo               the pet's states on a loop: no account, no daemon, no network
//   electron . --demo --still       the demo pet says hello once, then idles (to measure idle cost)
//   electron . --base=<url>         another Nemini site (a dev server); passed to nemd
//   electron . --nemd=<path>        a nemd binary other than daemon/bin/nemd
//   electron . --no-external        development: log outside links instead of opening them
//   electron . --background         no window at start, just the pet and the tray (how login starts it)
import { existsSync } from 'node:fs'
import { join } from 'node:path'
import { app, type BrowserWindow, dialog, Menu, Notification, shell } from 'electron'
import { autostartEnabled, BACKGROUND_FLAG, setAutostart } from './autostart'
import { Demo } from './demo'
import { inactive, present } from './inactive'
import { createMainWindow } from './mainWindow'
import { type Companion, Nemd, NemdError, type NemdStatus, nemdBinary } from './nemd'
import { notifyFrame } from './notify'
import { Overlay } from './overlay'
import { openPairWindow } from './pairWindow'
import { openPrefsWindow, type PrefsState, pushPrefs } from './prefsWindow'
import { cardLine, type PeekCard, type PeekEvent, peekFace, peekLine } from '../shared/peekText'
import { type Presence, PresenceWatch } from './presence'
import { loadSettings, saveSettings, type Settings } from './settings'
import { AppTray } from './tray'
import { startUpdates, type Updates } from './updater'
import { type Lang, pickLang, type T, translator } from '../shared/i18n'

const VERSION = app.getVersion()
const argv = process.argv.slice(1)
function flag(name: string): string | undefined {
  const pre = `--${name}=`
  return argv.find(a => a.startsWith(pre))?.slice(pre.length)
}
const DEMO = argv.includes('--demo')
const BASE = flag('base')
const DEFAULT_BASE = 'https://nemini.ai'
/** key failures that mean this key is dead for good: forget it, the human pairs again */
const DEAD_KEY = new Set(['key_revoked', 'key_expired', 'invalid_api_key', 'api_key_expired'])

/**
 * Linux on Wayland: the pet needs X11 (through XWayland). Native Wayland gives an app no global
 * position (`setPosition`, the cursor's screen point: the drag), no input shape (click-through)
 * and no reliable always-on-top, and Electron runs native since 38.2. So start once more with
 * `--ozone-platform=x11`; launchers that already pass it (dev.sh, the Linux packages) skip this.
 * No XWayland (no DISPLAY): stay native; the pet then can't be placed or dragged.
 */
function relaunchOnX11(): boolean {
  if (process.platform !== 'linux') return false
  const wayland = process.env.XDG_SESSION_TYPE === 'wayland' || !!process.env.WAYLAND_DISPLAY
  if (!wayland || !process.env.DISPLAY) return false
  if (process.argv.some(a => a.startsWith('--ozone-platform'))) return false
  app.relaunch({ args: [...process.argv.slice(1), '--ozone-platform=x11'] })
  app.exit(0)
  return true
}
const RELAUNCHING = relaunchOnX11()

// The demo gets its own profile, so it can run next to the real app.
// Development runs (not packaged) keep their own state: their own Electron profile here and their
// own nemd config and socket (profile "dev"), so a dev session never changes the installed app
// (one left the installed app on the dev site, 2026-10-19).
const DEV = !app.isPackaged
if (DEV) app.setPath('userData', join(app.getPath('appData'), 'Nemini-dev'))
if (DEMO) app.setPath('userData', join(app.getPath('userData'), 'demo'))
if (process.platform === 'win32') app.setAppUserModelId('ai.nemini.desktop')

const dist = join(__dirname, '..')
// packaged, the assets are unpacked next to the archive (electron-builder.yml asarUnpack): the
// notification daemon on Linux reads the icon by path
const assets = join(app.isPackaged ? app.getAppPath().replace(/app\.asar$/, 'app.asar.unpacked') : app.getAppPath(), 'assets')

interface PetInfo { pet: { muted?: boolean } | null, egg: boolean }
interface PairResult { status: NemdStatus, companions: Companion[] }

let settings: Settings = { petVisible: true }
let quitting = false
let main: BrowserWindow | null = null
let overlay: Overlay | null = null
let tray: AppTray | null = null
let demo: Demo | null = null
let nemd: Nemd | null = null
let nemdProblem = ''
let status: NemdStatus | null = null
/** refreshes the tray and the settings when Peek's pause ends */
let peekResumeTimer: ReturnType<typeof setTimeout> | undefined
let petInfo: PetInfo | null = null
let companions: Companion[] = []
let presence: PresenceWatch | null = null
let reconnectMs = 1000
/** this computer's key and the site it belongs to (from nemd); the window signs its API calls with it */
let key: { key: string, base: string } | null = null
/** the shell's language (the system's, if the app has it): set once the app is ready */
let lang: Lang = 'en'
let t: T = translator('en')
/** updates (packaged builds): the downloaded version waiting for a restart */
let updates: Updates | null = null
let updateReady = ''
/** the pair window opens by itself once per run, when the app starts unpaired */
let pairAsked = false

function base(): string { return status?.base || BASE || DEFAULT_BASE }

// --- what the human sees ---------------------------------------------------------------------

function companionName(): string {
  return companions.find(c => c.uuid === status?.companion)?.name || 'Nemini'
}

function statusLine(): string {
  if (DEMO) return t('status.demo')
  if (!nemd || !status) return nemdProblem ? t('status.no_daemon') : t('status.starting')
  if (status.keychain) return t('status.no_keychain')
  if (!status.paired) return t('status.not_paired')
  if (!status.companion) return t('status.no_companion')
  switch (status.socket) {
    case 'open': return t('status.with', { name: companionName() })
    case 'connecting':
    case 'retrying': return t('status.connecting')
    case 'gone': return t('status.gone')
    case 'auth_failed': return t('status.refused')
  }
  return status.presence && status.presence !== 'active' ? t('status.away') : t('status.paired_with', { name: companionName() })
}

function petShouldShow(): boolean {
  if (!settings.petVisible) return false
  if (DEMO) return true
  return !!status?.paired && !!petInfo?.pet && !petInfo.pet.muted
}

function refresh() {
  overlay?.setVisible(petShouldShow())
  clearTimeout(peekResumeTimer)
  const resumeIn = peekPaused() ? new Date(status!.peek!.paused_until!).getTime() - Date.now() + 1000 : 0
  if (resumeIn > 0) peekResumeTimer = setTimeout(refresh, Math.min(resumeIn, 2 ** 31 - 1))
  tray?.update({
    status: statusLine(),
    paired: !!status?.paired,
    petVisible: settings.petVisible,
    autostart: !DEMO && safeAutostartEnabled(),
    update: updateReady || undefined,
    companions: companions.map(c => ({ ...c, selected: c.uuid === status?.companion })),
    peek: status?.peek?.on ? { paused: peekPaused() } : undefined,
  })
  overlay?.send('overlay:peek', { on: !!status?.peek?.on && !peekPaused() })
  pushPrefs(prefsState())
  void askMassDelete()
}

// Many files deleted from her desktop's folder at once: nemd waits for the human's answer.
let askingMassDelete = false
async function askMassDelete() {
  const d = status?.desk
  if (askingMassDelete || !nemd || d?.state !== 'waiting' || d.detail !== 'mass_delete') return
  askingMassDelete = true
  try {
    const name = companions.find(c => c.uuid === status?.desk_companion)?.name || companionName()
    const { response } = await dialog.showMessageBox({
      type: 'warning', title: 'Nemini',
      buttons: [t('dialog.mass_delete_yes', { name }), t('dialog.mass_delete_no')], defaultId: 1, cancelId: 1,
      message: t('dialog.mass_delete_q', { count: d.count || 0, name }),
      detail: t('dialog.mass_delete_detail', { name }),
    })
    status = await nemd.call<NemdStatus>('desk.answer', { allow: response === 0 })
  }
  catch { /* the sync asks again on its next pass */ }
  finally {
    askingMassDelete = false
  }
}

// --- Peek: the pet's word on what you're up to (nemd's peek, off by default) -----------------

function peekPaused(): boolean {
  const until = status?.peek?.paused_until
  return !!until && new Date(until).getTime() > Date.now()
}

/** the pet says it (with a face for a few seconds); with the pet hidden, a notification only when asked for */
function petSay(line: string, face: string, asked: boolean) {
  if (!line) return
  if (overlay && petShouldShow()) {
    if (face) overlay.send('overlay:frame', { type: 'mood', face })
    overlay.send('overlay:say', line)
  }
  else if (asked && Notification.isSupported()) {
    new Notification({ title: companionName(), body: line, icon: join(assets, 'icon.png') }).show()
  }
}

/** a Peek moment her pet couldn't answer (offline, an older site): the app's own line */
function onPeek(ev: PeekEvent) {
  petSay(peekLine(t, ev), peekFace(ev), false)
}

/** "Judge me": her pet answers in its own voice (the line comes as a pet frame, or as a
 *  notification with the pet hidden); offline or no line: the app's own report card */
async function judgeMe() {
  if (!nemd) return
  try {
    const r = await nemd.call<{ said: boolean, line?: string, card: PeekCard }>('peek.judge')
    if (r.said) {
      if (!(overlay && petShouldShow()) && r.line) petSay(r.line, '', true)
      return
    }
    petSay(cardLine(t, r.card), 'thinking', true)
  }
  catch { /* nemd isn't there, or Peek is off */ }
}

async function pausePeek(minutes: number) {
  try {
    status = await nemd!.call<NemdStatus>('peek.pause', { minutes })
    refresh()
  }
  catch { /* nemd isn't there */ }
}

// --- the app's own settings (main/prefsWindow.ts) -------------------------------------------

const SOURCE_URL = 'https://github.com/PixiLab-ai/nem-agent-public'

function hostOf(u: string): string {
  try { return new URL(u).host }
  catch { return u }
}

function prefsState(): PrefsState {
  const deskName = companions.find(c => c.uuid === status?.desk_companion)?.name || companionName()
  return {
    version: VERSION,
    site: hostOf(base()),
    paired: !!status?.paired,
    companion: status?.companion ? companionName() : '',
    pet: settings.petVisible,
    autostart: !DEMO && safeAutostartEnabled(),
    autostartAvailable: !DEMO,
    desk: status?.desk_root ? { root: status.desk_root, name: deskName, sync: status.desk || null } : null,
    update: updateReady,
    updatable: !!updates,
    peek: { on: !!status?.peek?.on, pausedUntil: peekPaused() ? status!.peek!.paused_until! : '', mode: status?.peek?.mode || '' },
  }
}

function openPrefs() {
  if (DEMO) return
  openPrefsWindow({
    preload: join(dist, 'preload/prefs.cjs'),
    page: join(dist, 'prefs/index.html'),
    lang,
    t,
    act: {
      state: prefsState,
      setPet: (on) => { settings.petVisible = on; saveSettings(settings); refresh() },
      setAutostart: (on) => {
        try {
          setAutostart(on)
          return null
        }
        catch (e) {
          return t('dialog.autostart_failed', { error: (e as Error).message })
        }
        finally {
          refresh()
        }
      },
      chooseFolder: chooseDeskFolder,
      openFolder: () => { if (status?.desk_root) void shell.openPath(status.desk_root) },
      syncNow: () => { void nemd?.call('desk.now').catch(() => {}) },
      stopFolder: stopDeskFolder,
      checkUpdates: async () => (updates ? updates.check() : 'unavailable'),
      installUpdate: () => updates?.install(),
      openSource: () => void shell.openExternal(SOURCE_URL),
      signOut: () => void unpair(),
      pair: pairWindow,
      setPeek: async (on) => {
        if (!nemd) return { ok: false, error: 'disconnected' }
        try {
          status = await nemd.call<NemdStatus>('peek.set', { on })
          refresh()
          return { ok: true }
        }
        catch (e) {
          return { ok: false, error: e instanceof NemdError ? e.code : 'disconnected' }
        }
      },
      pausePeek: (minutes) => void pausePeek(minutes),
      forgetPeek: async () => {
        try {
          await nemd?.call('peek.forget')
          return true
        }
        catch {
          return false
        }
      },
      peekToday: async () => {
        try {
          const r = await nemd?.call<{ today: Record<string, number> }>('peek.get')
          return r?.today || {}
        }
        catch {
          return {}
        }
      },
      judge: () => void judgeMe(),
    },
  })
}

/** Her desktop's folder: the picker (an empty folder, or a new one), then nemd's check and claim. */
async function chooseDeskFolder(win: BrowserWindow): Promise<{ ok: boolean, error?: string }> {
  if (!nemd) return { ok: false, error: 'disconnected' }
  const home = app.getPath('home')
  const suggested = join(home, 'Nemini')
  const r = await dialog.showOpenDialog(win, {
    title: t('prefs.desk.pick_title', { name: companionName() }),
    buttonLabel: t('prefs.desk.pick_button'),
    defaultPath: existsSync(suggested) ? suggested : home,
    properties: ['openDirectory', 'createDirectory', 'promptToCreate'],
  })
  if (r.canceled || !r.filePaths[0]) return { ok: false, error: 'canceled' }
  try {
    status = await nemd.call<NemdStatus>('desk.set', { root: r.filePaths[0] })
    refresh()
    return { ok: true }
  }
  catch (e) {
    return { ok: false, error: e instanceof NemdError ? e.code : 'disconnected' }
  }
}

/** Stop syncing: the folder and its files stay. */
async function stopDeskFolder(win: BrowserWindow): Promise<{ ok: boolean, error?: string }> {
  if (!nemd) return { ok: false, error: 'disconnected' }
  const { response } = await dialog.showMessageBox(win, {
    type: 'question', title: 'Nemini', buttons: [t('prefs.desk.stop'), t('dialog.cancel')], defaultId: 1, cancelId: 1,
    message: t('prefs.desk.stop_q'),
    detail: t('prefs.desk.stop_detail'),
  })
  if (response !== 0) return { ok: false, error: 'canceled' }
  try {
    status = await nemd.call<NemdStatus>('desk.unset')
    refresh()
    return { ok: true }
  }
  catch (e) {
    return { ok: false, error: e instanceof NemdError ? e.code : 'disconnected' }
  }
}

function safeAutostartEnabled(): boolean {
  try { return autostartEnabled() }
  catch { return false }
}

// --- the main window -------------------------------------------------------------------------

function showMain() {
  if (DEMO) return
  if (!main || main.isDestroyed()) {
    const site = base()
    main = createMainWindow({
      base: site,
      userAgentTag: `NeminiDesktop/${VERSION}`,
      // a key is only ever sent to the site it was made on
      key: () => (key && key.base === site ? key.key : null),
      noExternal: argv.includes('--no-external'),
      onAppLink: (what) => { if (what === 'settings') openPrefs() },
    })
    main.on('close', (e) => {
      if (quitting) return
      e.preventDefault()
      main?.hide() // the app lives on in the tray, with the pet
    })
    return
  }
  if (main.isMinimized()) main.restore()
  present(main)
  if (!inactive) main.focus()
}

/** The window's sign-in follows the key: reload it when the key comes or goes. */
function reloadMain() {
  if (main && !main.isDestroyed()) main.webContents.reload()
}

async function refreshKey() {
  if (!nemd) { key = null; return }
  try {
    key = await nemd.call<{ key: string, base: string }>('key')
  }
  catch {
    key = null // not paired, or the keychain is unavailable (the tray says which)
  }
}

// --- pairing ---------------------------------------------------------------------------------

function pairWindow() {
  if (!nemd || DEMO) return
  openPairWindow({
    preload: join(dist, 'preload/pair.cjs'),
    page: join(dist, 'pair/index.html'),
    base: base(),
    pair: async (token) => {
      try {
        await afterPair(await nemd!.call<PairResult>('pair', { token }))
        return { ok: true }
      }
      catch (e) {
        return { ok: false, error: e instanceof NemdError ? e.code : 'network' }
      }
    },
    onPaired: () => { showMain(); reloadMain() },
    lang,
    t,
  })
}

async function afterPair(r: PairResult) {
  status = r.status
  companions = r.companions || []
  await refreshKey()
  await refreshPet()
  refresh()
}

async function unpair() {
  if (!nemd || !status) return
  const { response } = await dialog.showMessageBox({
    type: 'question', title: 'Nemini', buttons: [t('dialog.unpair'), t('dialog.cancel')], defaultId: 1, cancelId: 1,
    message: t('dialog.unpair_q'),
    detail: t('dialog.unpair_detail'),
  })
  if (response !== 0) return
  try {
    status = await nemd.call<NemdStatus>('unpair')
  }
  catch (e) {
    dialog.showErrorBox('Nemini', t('dialog.unpair_failed', { error: (e as Error).message }))
    return
  }
  key = null
  petInfo = null
  reloadMain()
  refresh()
  // the key itself is still valid until revoked, and only a browser session revokes keys
  const after = await dialog.showMessageBox({
    type: 'info', title: 'Nemini', buttons: [t('dialog.open_keys'), t('dialog.close')], defaultId: 0, cancelId: 1,
    message: t('dialog.unpaired'),
    detail: t('dialog.unpaired_detail'),
  })
  if (after.response === 0) void shell.openExternal(`${base()}/me/settings/app`)
}

// --- nemd ------------------------------------------------------------------------------------

async function refreshPet() {
  if (!nemd || !status?.paired || !status.companion) { petInfo = null; return }
  try {
    const info = await nemd.call<PetInfo>('pet.get')
    petInfo = { pet: info.pet, egg: !!info.egg }
    overlay?.send('overlay:info', { ...petInfo, owner: companionName() })
  }
  catch (e) {
    console.error('pet.get:', (e as Error).message)
  }
}

async function refreshCompanions() {
  if (!nemd || !status?.paired) { companions = []; return }
  try {
    companions = (await nemd.call<{ companions: Companion[] }>('companions')).companions || []
  }
  catch (e) {
    console.error('companions:', (e as Error).message)
  }
}

/** A revoked or expired key: forget it here too; the window shows itself unpaired. */
async function forgetDeadKey(code: string) {
  if (!nemd || !DEAD_KEY.has(code) || !status?.paired) return
  try {
    status = await nemd.call<NemdStatus>('unpair')
  }
  catch { /* the tray still says the key was refused */ }
  key = null
  petInfo = null
  reloadMain()
  refresh()
}

function onNemdEvent(event: string, data: Record<string, unknown>) {
  switch (event) {
    case 'chat': {
      overlay?.send('overlay:frame', data)
      if (data.type === 'pet' && (data.kind === 'hatch' || data.kind === 'update') && data.pet) {
        petInfo = { pet: data.pet as PetInfo['pet'], egg: false }
        refresh()
      }
      notifyFrame(data, {
        name: companionName(),
        looking: !!main && !main.isDestroyed() && main.isFocused(),
        icon: join(assets, 'icon.png'),
        onClick: showMain,
        t,
      })
      break
    }
    case 'socket':
      if (status) {
        status.socket = String(data.state || '')
        status.reason = String(data.reason || '')
      }
      if (data.state === 'auth_failed') void forgetDeadKey(String(data.reason || ''))
      if (data.state === 'open') void refreshPet() // the pet may have changed while the socket was closed
      refresh()
      break
    case 'auth':
      void forgetDeadKey(String(data.error || ''))
      break
    case 'peek':
      onPeek(data as unknown as PeekEvent)
      break
    case 'status':
      // nemd's own news (her desktop's folder set or forgotten): the tray and the settings follow
      status = data as unknown as NemdStatus
      refresh()
      break
    case 'paired':
    case 'unpaired':
      void (async () => {
        status = await nemd!.call<NemdStatus>('status')
        const had = key?.key
        await refreshKey()
        if (key?.key !== had) reloadMain()
        await refreshCompanions()
        await refreshPet()
        refresh()
      })()
      break
  }
}

async function connectNemd() {
  if (!nemd) return
  try {
    status = await nemd.start()
    if (app.isPackaged && !nemd.ours && status.version !== VERSION) {
      // an nemd of another version still running (from before an update): stop it; the
      // disconnect brings us back here, and this time ours starts
      console.log(`[nem] nemd ${status.version} running, this app ships ${VERSION}: replacing it`)
      await nemd.call('shutdown').catch(() => {})
      return
    }
    reconnectMs = 1000
    nemdProblem = ''
    if (presence?.current) status = await nemd.call<NemdStatus>('presence.set', { state: presence.current })
    await refreshKey()
    await refreshCompanions()
    await refreshPet()
  }
  catch (e) {
    nemdProblem = (e as Error).message
    console.error('nemd:', nemdProblem)
    setTimeout(() => void connectNemd(), reconnectMs)
    reconnectMs = Math.min(reconnectMs * 2, 30_000)
  }
  refresh()
}

function startNemd(): boolean {
  try {
    nemd = new Nemd(nemdBinary(flag('nemd')), BASE, DEV ? 'dev' : undefined)
  }
  catch (e) {
    nemdProblem = (e as Error).message
    console.error(nemdProblem)
    return false
  }
  nemd.on('event', onNemdEvent)
  nemd.on('disconnected', () => {
    status = null
    refresh()
    setTimeout(() => void connectNemd(), reconnectMs)
  })
  presence = new PresenceWatch((p: Presence) => {
    nemd?.call<NemdStatus>('presence.set', { state: p })
      .then((st) => { status = st; refresh() })
      .catch(() => { /* not connected yet: connectNemd sends it */ })
  })
  presence.start()
  return true
}

// --- the app ---------------------------------------------------------------------------------

/**
 * Packaged builds get no default menu on Linux and Windows: no DevTools shortcut on the window that
 * carries the key, no reload; copy and paste work without a menu there. macOS keeps a minimal one
 * (Quit, and the Edit menu its copy / paste shortcuts live in). Development keeps Electron's menu.
 */
function productionMenu() {
  if (!app.isPackaged) return
  if (process.platform !== 'darwin') {
    Menu.setApplicationMenu(null)
    return
  }
  Menu.setApplicationMenu(Menu.buildFromTemplate([
    { role: 'appMenu', submenu: [{ role: 'about' }, { type: 'separator' }, { label: t('tray.settings'), accelerator: 'Cmd+,', click: () => openPrefs() }, { type: 'separator' }, { role: 'hide' }, { role: 'hideOthers' }, { role: 'unhide' }, { type: 'separator' }, { role: 'quit' }] },
    { role: 'editMenu' },
    { role: 'windowMenu' },
  ]))
}

async function start() {
  lang = pickLang(app.getLocale())
  t = translator(lang)
  productionMenu()
  settings = loadSettings()
  overlay = new Overlay({
    preload: join(dist, 'preload/overlay.cjs'),
    page: join(dist, 'overlay/index.html'),
    saved: settings.position,
    lang,
    onMoved: (display, at) => { settings.position = { display, at }; saveSettings(settings) },
    onTap: async () => {
      if (demo) return demo.tap()
      return nemd?.call('pet.pet').catch(() => null) ?? null
    },
  })
  tray = new AppTray(join(assets, 'tray.png'), { status: statusLine(), paired: false, petVisible: settings.petVisible, autostart: !DEMO && safeAutostartEnabled(), demo: DEMO, companions: [] }, {
    open: showMain,
    settings: openPrefs,
    togglePet: (on) => { settings.petVisible = on; saveSettings(settings); refresh() },
    toggleAutostart: (on) => {
      try { setAutostart(on) }
      catch (e) { dialog.showErrorBox('Nemini', t('dialog.autostart_failed', { error: (e as Error).message })) }
      refresh()
    },
    pair: pairWindow,
    unpair: () => void unpair(),
    installUpdate: () => updates?.install(),
    peekPause: (minutes) => void pausePeek(minutes),
    judge: () => void judgeMe(),
    select: (uuid) => {
      nemd?.call<NemdStatus>('companion.select', { uuid })
        .then(async (st) => { status = st; await refreshPet(); refresh() })
        .catch(e => dialog.showErrorBox('Nemini', (e as Error).message))
    },
    quit: () => app.quit(),
  }, t)
  if (DEMO) {
    demo = new Demo(overlay, argv.includes('--still'))
    demo.start()
    return
  }
  // nemd first: the window opens on the site nemd is paired with, signed with its key
  if (startNemd()) await connectNemd()
  // started at login: just the pet and the tray, unless there's nothing to show yet
  if (!argv.includes(BACKGROUND_FLAG) || !status?.paired) showMain()
  if (status && !status.paired && !pairAsked) {
    pairAsked = true
    pairWindow()
  }
  updates = startUpdates((version) => {
    updateReady = version
    refresh()
    if (Notification.isSupported()) new Notification({ title: t('notify.update_title', { version }), body: t('notify.update_body'), icon: join(assets, 'icon.png') }).show()
  })
}

if (RELAUNCHING) {
  // exiting: the X11 instance takes over
}
else if (!app.requestSingleInstanceLock()) {
  app.quit()
}
else {
  app.on('second-instance', () => showMain())
  app.on('activate', () => showMain())
  app.on('window-all-closed', () => { /* stay in the tray */ })
  app.on('before-quit', () => {
    quitting = true
    demo?.stop()
    presence?.stop()
    if (nemd) {
      void nemd.call('presence.set', { state: 'away' }, 1000).catch(() => {})
      nemd.stop()
    }
  })
  void app.whenReady().then(start)
}
