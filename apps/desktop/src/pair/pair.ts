// The pair window's page: paste a key, the app checks it with the site and keeps it.
import { pickLang, translator } from '../shared/i18n'
import { STRINGS } from '../shared/strings'
import type { PairApi } from '../preload/pair'

declare global {
  interface Window { pairing: PairApi }
}

const params = new URLSearchParams(location.search)
const lang = pickLang(params.get('lang'))
const t = translator(lang)
document.documentElement.lang = lang
document.title = t('pair.title')
for (const el of document.querySelectorAll<HTMLElement>('[data-t]')) el.textContent = t(el.dataset.t || '')

const $ = <T extends HTMLElement>(id: string) => document.getElementById(id) as T
const form = $<HTMLFormElement>('form')
const input = $<HTMLInputElement>('key')
const button = $<HTMLButtonElement>('pair')
const msg = $<HTMLParagraphElement>('msg')

$<HTMLParagraphElement>('site').textContent = t('pair.site', { site: params.get('site') || '' })
$<HTMLButtonElement>('open').addEventListener('click', () => window.pairing.keysPage())

/** an error code from the app or the site, in words (unknown codes are quoted) */
function explain(code: string): string {
  return `pair.err.${code}` in STRINGS ? t(`pair.err.${code}`) : t('pair.err.other', { code: code || '?' })
}

function say(text: string, kind: '' | 'bad' | 'good' = '') {
  msg.textContent = text
  msg.className = kind
}

form.addEventListener('submit', async (e) => {
  e.preventDefault()
  const token = input.value.trim()
  if (!token) return
  button.disabled = true
  say(t('pair.checking'))
  try {
    const r = await window.pairing.submit(token)
    if (r.ok) {
      input.value = ''
      say(t('pair.ok'), 'good')
      return
    }
    say(explain(r.error || ''), 'bad')
  }
  catch {
    say(explain('disconnected'), 'bad')
  }
  finally {
    button.disabled = false
  }
})

input.focus()
