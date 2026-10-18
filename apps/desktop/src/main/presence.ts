// Is the human at this computer? An open app counts as "here" only while they're active (input
// in the last few minutes, screen unlocked, machine awake). nemd opens her socket only then.
import { powerMonitor } from 'electron'

export type Presence = 'active' | 'idle' | 'away' | 'locked'

/** No input for this long = idle. */
const IDLE_AFTER_S = 5 * 60
/** Re-check slowly while active, quickly while idle (to reopen her socket soon after the human is back). */
const CHECK_ACTIVE_MS = 15_000
const CHECK_IDLE_MS = 2_000

export class PresenceWatch {
  private state: Presence | null = null
  private timer: NodeJS.Timeout | null = null
  private away = false

  constructor(private onChange: (p: Presence) => void) {}

  start() {
    powerMonitor.on('lock-screen', this.onLock)
    powerMonitor.on('unlock-screen', this.onWake)
    powerMonitor.on('suspend', this.onSuspend)
    powerMonitor.on('resume', this.onWake)
    this.check()
  }

  stop() {
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
    powerMonitor.off('lock-screen', this.onLock)
    powerMonitor.off('unlock-screen', this.onWake)
    powerMonitor.off('suspend', this.onSuspend)
    powerMonitor.off('resume', this.onWake)
  }

  get current(): Presence | null { return this.state }

  private onLock = () => this.set('locked')
  private onSuspend = () => { this.away = true; this.set('away') }
  private onWake = () => { this.away = false; this.check() }

  private check = () => {
    if (this.timer) clearTimeout(this.timer)
    if (!this.away) {
      const s = powerMonitor.getSystemIdleState(IDLE_AFTER_S)
      // 'unknown': the platform can't tell (some Wayland sessions) — an open app then counts as active
      this.set(s === 'idle' ? 'idle' : s === 'locked' ? 'locked' : 'active')
    }
    this.timer = setTimeout(this.check, this.state === 'active' ? CHECK_ACTIVE_MS : CHECK_IDLE_MS)
  }

  private set(p: Presence) {
    if (p === this.state) return
    this.state = p
    this.onChange(p)
  }
}
