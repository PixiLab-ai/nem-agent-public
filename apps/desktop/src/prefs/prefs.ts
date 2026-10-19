// The settings window's page: the app's own settings (main/prefsWindow.ts), drawn from one state.
import { pickLang, translator } from '../shared/i18n'
import { STRINGS } from '../shared/strings'
import type { PrefsApi } from '../preload/prefs'
import type { PrefsState } from '../main/prefsWindow'

declare global {
  interface Window { prefs: PrefsApi }
}

const params = new URLSearchParams(location.search)
const lang = pickLang(params.get('lang'))
const t = translator(lang)
document.documentElement.lang = lang
document.title = t('prefs.title')
for (const el of document.querySelectorAll<HTMLElement>('[data-t]')) el.textContent = t(el.dataset.t || '')

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T
const btn = (id: string) => $<HTMLButtonElement>(id)

function say(id: string, text: string, kind: '' | 'bad' | 'good' = '') {
  const el = $(id)
  el.textContent = text
  el.className = `msg ${kind}`.trim()
}

/** a folder refusal (nemd's desksync codes) in words */
function folderError(code: string): string {
  return `prefs.desk.err.${code}` in STRINGS ? t(`prefs.desk.err.${code}`) : t('prefs.desk.err.other', { code: code || '?' })
}

let state: PrefsState | null = null

/** what the folder sync is doing, in words */
function syncLine(sync: NonNullable<PrefsState['desk']>['sync']): string {
  if (!sync) return t('prefs.desk.state.starting')
  switch (sync.state) {
    case 'downloading': return t('prefs.desk.state.downloading', { done: sync.done || 0, total: sync.total || 0 })
    case 'up_to_date': return t('prefs.desk.state.up_to_date')
    case 'syncing': return t('prefs.desk.state.syncing')
    case 'waiting': return t('prefs.desk.state.waiting', { count: sync.count || 0 })
    case 'paused': return t('prefs.desk.state.paused')
    case 'stopped': return sync.detail === 'companion_gone' ? t('prefs.desk.state.companion_gone') : t('prefs.desk.state.auth_failed')
    case 'error': return t('prefs.desk.state.error', { detail: sync.detail || '?' })
  }
  return t('prefs.desk.state.starting')
}

function render(s: PrefsState) {
  state = s
  const name = s.desk?.name || s.companion || 'Nemini'
  $('desk-h').textContent = t('prefs.desk.h', { name })
  $('desk-p').textContent = t('prefs.desk.p', { name })
  $('desk-where').textContent = s.desk ? s.desk.root : ''
  $('desk-state').textContent = s.desk ? syncLine(s.desk.sync) : (s.paired && s.companion ? t('prefs.desk.none') : t('prefs.desk.no_companion'))
  btn('desk-choose').hidden = !!s.desk
  btn('desk-choose').disabled = !s.paired || !s.companion
  btn('desk-open').hidden = !s.desk
  btn('desk-stop').hidden = !s.desk

  $<HTMLInputElement>('pet').checked = s.pet
  const auto = $<HTMLInputElement>('autostart')
  auto.checked = s.autostart
  auto.disabled = !s.autostartAvailable

  $('account').textContent = s.paired
    ? t('prefs.account.signed', { site: s.site, name: s.companion || '—' })
    : t('prefs.account.not_paired')
  btn('signout').hidden = !s.paired
  btn('pair').hidden = s.paired

  $('version').textContent = t('prefs.about.version', { version: s.version })
  btn('updates').hidden = !s.updatable || !!s.update
  btn('install').hidden = !s.update
  if (s.update) say('update-msg', t('prefs.about.ready', { version: s.update }), 'good')
  else if (!s.updatable) say('update-msg', t('prefs.about.no_updates'))
}

btn('desk-choose').addEventListener('click', async () => {
  btn('desk-choose').disabled = true
  say('desk-msg', '')
  try {
    const r = await window.prefs.chooseFolder()
    if (!r.ok && r.error && r.error !== 'canceled') say('desk-msg', folderError(r.error), 'bad')
  }
  finally {
    btn('desk-choose').disabled = !state?.paired || !state?.companion
  }
})
btn('desk-open').addEventListener('click', () => window.prefs.openFolder())
btn('desk-stop').addEventListener('click', async () => {
  const r = await window.prefs.stopFolder()
  if (!r.ok && r.error && r.error !== 'canceled') say('desk-msg', folderError(r.error), 'bad')
  else say('desk-msg', '')
})
$<HTMLInputElement>('pet').addEventListener('change', async (e) => {
  render(await window.prefs.setPet((e.target as HTMLInputElement).checked))
})
$<HTMLInputElement>('autostart').addEventListener('change', async (e) => {
  const r = await window.prefs.setAutostart((e.target as HTMLInputElement).checked)
  say('local-msg', r.error || '', r.error ? 'bad' : '')
  render(r.state)
})
btn('signout').addEventListener('click', () => window.prefs.signOut())
btn('pair').addEventListener('click', () => window.prefs.pair())
btn('install').addEventListener('click', () => window.prefs.installUpdate())
btn('source').addEventListener('click', () => window.prefs.openSource())
btn('updates').addEventListener('click', async () => {
  btn('updates').disabled = true
  say('update-msg', t('prefs.about.checking'))
  try {
    const r = await window.prefs.checkUpdates()
    if (r === 'none') say('update-msg', t('prefs.about.uptodate'), 'good')
    else if (r === 'available') say('update-msg', t('prefs.about.downloading'))
    else if (r === 'unavailable') say('update-msg', t('prefs.about.no_updates'))
    else say('update-msg', t('prefs.about.update_error'), 'bad')
  }
  finally {
    btn('updates').disabled = false
  }
})

window.prefs.onState(render)
void window.prefs.state().then(render)
