// Desktop notifications for her messages. While nemd's socket is open the server sends no web
// push (an open socket means "the chat is open somewhere"), so the app has to show these itself:
// her replies and the turns she starts on her own, and medals, while the main window isn't in
// front of the human.
import { Notification } from 'electron'
import type { T } from '../shared/i18n'

export interface NotifyContext {
  /** the companion's name (the title) */
  name: string
  /** true when the main window is focused: the page already shows everything */
  looking: boolean
  icon?: string
  onClick: () => void
  t: T
}

function excerpt(text: string, max = 180): string {
  const flat = text.replace(/[*_`#>~]+/g, '').replace(/\s+/g, ' ').trim()
  return flat.length > max ? `${flat.slice(0, max - 1).trimEnd()}…` : flat
}

function show(title: string, body: string, ctx: NotifyContext) {
  if (!Notification.isSupported() || !body) return
  const n = new Notification({ title, body, icon: ctx.icon, silent: false })
  n.on('click', ctx.onClick)
  n.show()
}

/** A chat frame from nemd; returns whether it notified. */
export function notifyFrame(f: Record<string, unknown>, ctx: NotifyContext): boolean {
  if (ctx.looking) return false
  if (f.type === 'turn_end') {
    if (f.error || f.quiet || typeof f.reply !== 'string' || !f.reply.trim()) return false
    show(ctx.name, excerpt(f.reply), ctx)
    return true
  }
  if (f.type === 'achievement' && typeof f.name === 'string') {
    show(ctx.name, excerpt(`${ctx.t('notify.unlocked', { name: f.name })}${typeof f.desc === 'string' && f.desc ? ` — ${f.desc}` : ''}`), ctx)
    return true
  }
  return false
}
