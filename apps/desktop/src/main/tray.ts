// The tray icon: the app's home while the main window is closed. Everything here is
// device-local; the account-wide pet switch stays in the site's Settings.
import { Menu, nativeImage, Tray } from 'electron'
import type { T } from '../shared/i18n'

export interface TrayState {
  /** one line on what the app is doing ("Connected", "Not paired", …) */
  status: string
  paired: boolean
  petVisible: boolean
  /** start at login (device-local) */
  autostart: boolean
  demo: boolean
  /** a downloaded update, waiting for a restart (its version) */
  update?: string
  /** the account's companions, when there's a choice to make */
  companions: { uuid: string, name: string, selected: boolean }[]
}

export interface TrayActions {
  open: () => void
  /** the app's own settings window */
  settings: () => void
  togglePet: (visible: boolean) => void
  toggleAutostart: (on: boolean) => void
  pair: () => void
  unpair: () => void
  select: (uuid: string) => void
  installUpdate: () => void
  quit: () => void
}

export class AppTray {
  private tray: Tray
  private state: TrayState

  constructor(icon: string, initial: TrayState, private act: TrayActions, private t: T) {
    const img = nativeImage.createFromPath(icon)
    this.tray = new Tray(img.isEmpty() ? img : img.resize({ width: 22, height: 22 }))
    this.tray.setToolTip('Nemini')
    this.tray.on('click', () => act.open()) // Windows / some Linux trays; the menu covers the rest
    this.state = initial
    this.render()
  }

  update(patch: Partial<TrayState>) {
    this.state = { ...this.state, ...patch }
    this.render()
  }

  private render() {
    const s = this.state
    const menu = Menu.buildFromTemplate([
      { label: s.status, enabled: false },
      ...(s.update ? [{ label: this.t('tray.update', { version: s.update }), click: () => this.act.installUpdate() }] : []),
      { type: 'separator' },
      { label: this.t('tray.open'), enabled: !s.demo, click: () => this.act.open() },
      { label: this.t('tray.settings'), enabled: !s.demo, click: () => this.act.settings() },
      { label: this.t('tray.show_pet'), type: 'checkbox', checked: s.petVisible, click: item => this.act.togglePet(item.checked) },
      { label: this.t('tray.autostart'), type: 'checkbox', checked: s.autostart, enabled: !s.demo, click: item => this.act.toggleAutostart(item.checked) },
      ...(s.paired && s.companions.length > 1
        ? [{
            label: this.t('tray.companion'),
            submenu: s.companions.map(c => ({ label: c.name || c.uuid, type: 'radio' as const, checked: c.selected, click: () => this.act.select(c.uuid) })),
          }]
        : []),
      { type: 'separator' },
      s.paired
        ? { label: this.t('tray.unpair'), enabled: !s.demo, click: () => this.act.unpair() }
        : { label: this.t('tray.pair'), enabled: !s.demo, click: () => this.act.pair() },
      { type: 'separator' },
      { label: this.t('tray.quit'), click: () => this.act.quit() },
    ])
    this.tray.setContextMenu(menu)
    this.tray.setToolTip(`Nemini — ${s.status}`)
  }

  destroy() { this.tray.destroy() }
}
