// The one place that decides what counts as "the Nemini site" for this window.
export function originOf(url: string): string {
  try { return new URL(url).origin }
  catch { return '' }
}

export function sameSite(url: string, base: string): boolean {
  const o = originOf(url)
  return o !== '' && o === originOf(base)
}
