// `--inactive` (development and tests): the app's windows open without taking the keyboard focus,
// so a test run never types into the human's work (2026-10-19: a test window caught keystrokes).
import type { BrowserWindow } from 'electron'

const INACTIVE = process.argv.includes('--inactive')

/** shows a window: without the focus under --inactive */
export function present(win: BrowserWindow) {
  if (INACTIVE) win.showInactive()
  else win.show()
}

export const inactive = INACTIVE
