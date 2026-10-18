// The pair window's preload: our own bundled page, two calls out.
import { contextBridge, ipcRenderer } from 'electron'

const api = {
  /** check and store a key: `{ok: true}` or `{ok: false, error}` (an API error code, or not_a_key) */
  submit(token: string): Promise<{ ok: boolean, error?: string }> { return ipcRenderer.invoke('pair:submit', token) },
  /** the website's Settings → Keys, in the human's browser */
  keysPage() { ipcRenderer.send('pair:keys-page') },
}

export type PairApi = typeof api

contextBridge.exposeInMainWorld('pairing', api)
