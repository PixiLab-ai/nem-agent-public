// What the pet says about what you're up to (nemd's peek events), and the report card — shared by
// the main process (the pet's bubble) and the settings window (today's minutes).
import { STRINGS } from './strings'
import type { T } from './i18n'

export interface PeekEvent {
  kind: string // morning · back · late · long · switching · card
  category?: string
  minutes?: number
  count?: number
  /** a game's name: the pet may say it, it is never kept or sent */
  game?: string
  card?: PeekCard
}

export interface PeekCard {
  top: { category: string, minutes: number }[] | null
  late: number
  days: number
  grade: string
}

/** key.1, key.2… that exist */
function variants(base: string): string[] {
  const out: string[] = []
  for (let i = 1; i <= 6; i++) {
    if (`${base}.${i}` in STRINGS) out.push(`${base}.${i}`)
  }
  return out
}

export function duration(t: T, minutes: number): string {
  return minutes >= 60 ? t('peek.hours', { n: Math.round(minutes / 60) }) : t('peek.minutes', { n: Math.max(1, Math.round(minutes)) })
}

/** "18 h coding, 6 h gaming, 3 h with me" */
export function minutesList(t: T, items: { category: string, minutes: number }[]): string {
  return items
    .filter(i => `peek.cat.${i.category}` in STRINGS && i.minutes >= 1)
    .map(i => t('peek.item', { dur: duration(t, i.minutes), cat: t(`peek.cat.${i.category}`) }))
    .join(t('peek.sep'))
}

export function cardLine(t: T, c: PeekCard): string {
  const list = minutesList(t, c.top || [])
  if (!list) return t('peek.card.empty')
  if (!c.grade) return t('peek.card.nograde', { list })
  return c.late ? t('peek.card', { list, late: c.late, grade: c.grade }) : t('peek.card.nolate', { list, grade: c.grade })
}

/** one of the lines for this event (a game's own line when it has a name), '' when none fits */
export function peekLine(t: T, ev: PeekEvent): string {
  if (ev.kind === 'card') return ev.card ? cardLine(t, ev.card) : ''
  const cat = ev.category || ''
  const keys = (ev.game ? variants(`peek.${ev.kind}.${cat}_named`) : []).concat()
  const pool = keys.length ? keys : variants(`peek.${ev.kind}.${cat}`)
  const pick = pool.length ? pool : variants(`peek.${ev.kind}`)
  if (!pick.length) return ''
  const key = pick[Math.floor(Math.random() * pick.length)]
  return t(key, { hours: Math.max(1, Math.round((ev.minutes || 0) / 60)), count: ev.count || 0, game: ev.game || '' })
}

/** the pet's face for it (a badge on the sprite for a few seconds) */
export function peekFace(ev: PeekEvent): string {
  switch (ev.kind) {
    case 'morning':
    case 'back': return 'happy'
    case 'late': return 'asleep'
    case 'switching': return 'smug'
    case 'card': return 'thinking'
    case 'long':
      if (ev.category === 'gaming') return 'gaming'
      return ['coding', 'terminal', 'writing', 'creating'].includes(ev.category || '') ? 'focused' : 'surprised'
  }
  return ''
}
