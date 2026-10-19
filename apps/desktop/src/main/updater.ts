// Updates from the public repository's GitHub Releases (electron-builder.yml → publish).
// electron-updater reads the latest*.yml every release carries (version, files, sha512), downloads
// in the background and installs when the app quits — or right away, from the tray ("Restart to
// update"). Packaged builds only. A failed check (offline, the repository still private) is
// logged, never shown. NEM_NO_UPDATES=1 turns it off.
import type { UpdateCheck } from './prefsWindow'
import { app } from 'electron'
import { autoUpdater } from 'electron-updater'

const FIRST_CHECK_MS = 60_000
const EVERY_MS = 6 * 3600_000

export interface Updates {
  /** quit now and install the downloaded version */
  install: () => void
  /** look now (the settings window's button): a newer version is downloading, or there is none */
  check: () => Promise<UpdateCheck>
}

export function startUpdates(onReady: (version: string) => void): Updates | null {
  if (!app.isPackaged || process.env.NEM_NO_UPDATES) return null
  autoUpdater.logger = {
    info: (m?: unknown) => console.log('[update]', m),
    warn: (m?: unknown) => console.warn('[update]', m),
    error: (m?: unknown) => console.error('[update]', m),
    debug: () => {},
  }
  autoUpdater.autoDownload = true
  autoUpdater.autoInstallOnAppQuit = true
  autoUpdater.on('update-downloaded', info => onReady(info.version))
  // electron-updater logs its own failures through the logger above; these only keep a failed
  // check (offline, the repository still private: a 404) from surfacing anywhere else
  autoUpdater.on('error', () => {})
  const check = () => { autoUpdater.checkForUpdates().catch(() => {}) }
  setTimeout(check, FIRST_CHECK_MS)
  setInterval(check, EVERY_MS)
  return {
    install: () => autoUpdater.quitAndInstall(),
    check: async () => {
      try {
        const r = await autoUpdater.checkForUpdates()
        return r?.isUpdateAvailable ? 'available' : 'none'
      }
      catch {
        return 'error'
      }
    },
  }
}
