// The overlay's preload: a narrow bridge for our own bundled page (never remote content).
// It only relays what the pet needs: state in, hit boxes / hover / drag / tap out.
import { contextBridge, ipcRenderer } from 'electron'

const IN = new Set(['overlay:info', 'overlay:frame', 'overlay:say', 'overlay:visible'])

export interface HitRect { x: number, y: number, width: number, height: number }

const api = {
  /** Subscribe to a state channel from the main process; returns an unsubscribe. */
  on(channel: string, cb: (payload: unknown) => void): () => void {
    if (!IN.has(channel)) throw new Error(`overlay: unknown channel ${channel}`)
    const fn = (_e: unknown, payload: unknown) => cb(payload)
    ipcRenderer.on(channel, fn)
    return () => { ipcRenderer.removeListener(channel, fn) }
  },
  /** The pet's hit boxes, window-relative (shape mode). */
  hits(rects: HitRect[]) { ipcRenderer.send('overlay:hits', rects) },
  /** Whether the pointer is over something solid (forward mode). */
  hover(over: boolean) { ipcRenderer.send('overlay:hover', over) },
  grabStart(): Promise<void> { return ipcRenderer.invoke('overlay:grab-start') },
  grabEnd(): Promise<{ moved: boolean }> { return ipcRenderer.invoke('overlay:grab-end') },
  /** The human petted it: resolves with `{pet, line?}` from the site, or null. */
  tap(): Promise<unknown> { return ipcRenderer.invoke('overlay:tap') },
}

export type OverlayApi = typeof api

contextBridge.exposeInMainWorld('nem', api)
