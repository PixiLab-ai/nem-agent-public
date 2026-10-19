// The settings window's preload: our own bundled page, the calls it needs and nothing else.
import { contextBridge, ipcRenderer } from 'electron'
import type { PrefsState, UpdateCheck } from '../main/prefsWindow'

const api = {
  state(): Promise<PrefsState> { return ipcRenderer.invoke('prefs:state') },
  /** the state again whenever it changes (another window, the tray, nemd) */
  onState(cb: (s: PrefsState) => void) { ipcRenderer.on('prefs:changed', (_e, s: PrefsState) => cb(s)) },
  setPet(on: boolean): Promise<PrefsState> { return ipcRenderer.invoke('prefs:pet', on) },
  setAutostart(on: boolean): Promise<{ error: string | null, state: PrefsState }> { return ipcRenderer.invoke('prefs:autostart', on) },
  /** the folder picker, then nemd's check: `{ok}` or `{ok: false, error}` (a desksync code, or canceled) */
  chooseFolder(): Promise<{ ok: boolean, error?: string }> { return ipcRenderer.invoke('prefs:desk-choose') },
  openFolder() { ipcRenderer.send('prefs:desk-open') },
  stopFolder(): Promise<{ ok: boolean, error?: string }> { return ipcRenderer.invoke('prefs:desk-stop') },
  checkUpdates(): Promise<UpdateCheck> { return ipcRenderer.invoke('prefs:updates') },
  installUpdate() { ipcRenderer.send('prefs:install') },
  openSource() { ipcRenderer.send('prefs:source') },
  signOut() { ipcRenderer.send('prefs:signout') },
  pair() { ipcRenderer.send('prefs:pair') },
}

export type PrefsApi = typeof api

contextBridge.exposeInMainWorld('prefs', api)
