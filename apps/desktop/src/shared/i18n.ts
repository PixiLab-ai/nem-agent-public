// The shell speaks the Nemini web app's languages; the system's language picks one (English
// otherwise). The main window follows the same language on its own (the site reads the
// browser's, which Electron takes from the system).
import { STRINGS } from './strings'

export const LANGS = ['en', 'fr', 'de', 'zh', 'es', 'ja'] as const
export type Lang = typeof LANGS[number]
export type T = (key: string, vars?: Record<string, string | number>) => string

/** "fr-FR", "zh-Hans-CN", "pt-BR" → a language the app has, else English. */
export function pickLang(locale: string | null | undefined): Lang {
  const l = (locale || '').toLowerCase().split(/[-_]/)[0]
  return (LANGS as readonly string[]).includes(l) ? l as Lang : 'en'
}

export function translator(lang: Lang): T {
  const i = Math.max(0, LANGS.indexOf(lang))
  return (key, vars) => {
    const row = STRINGS[key]
    let s = row ? (row[i] || row[0]) : key
    if (vars) s = s.replace(/\{\{\s*(\w+)\s*\}\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m))
    return s
  }
}
