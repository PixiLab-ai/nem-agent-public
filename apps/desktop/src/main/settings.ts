// Device-local settings: what this machine shows, never anything the account knows about.
// "Hide the pet" here is local — the account-wide mute lives in the site's Settings and makes
// her prompt learn the human switched it off; hiding it on one desktop must not.
import { mkdirSync, readFileSync, renameSync, writeFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { app } from 'electron'

export interface Point { x: number, y: number }

export interface Settings {
  petVisible: boolean
  /** the "still running" notice was shown, the first time the window was closed */
  closeHinted?: boolean
  /** where the human last dropped the pet: the display (by id) and the window's top-left */
  position?: { display: number, at: Point }
}

function file() { return join(app.getPath('userData'), 'settings.json') }

export function loadSettings(): Settings {
  try {
    const raw = JSON.parse(readFileSync(file(), 'utf8')) as Partial<Settings>
    const p = raw.position
    const position = p && Number.isFinite(p.display) && Number.isFinite(p.at?.x) && Number.isFinite(p.at?.y) ? p : undefined
    return { petVisible: raw.petVisible !== false, position, closeHinted: raw.closeHinted === true }
  }
  catch {
    return { petVisible: true }
  }
}

export function saveSettings(s: Settings) {
  try {
    const p = file()
    mkdirSync(dirname(p), { recursive: true })
    writeFileSync(`${p}.tmp`, JSON.stringify(s, null, 2))
    renameSync(`${p}.tmp`, p)
  }
  catch (e) {
    console.error('settings: not saved:', e)
  }
}
