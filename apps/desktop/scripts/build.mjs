// Builds the shell: the main process and the preloads (overlay, pair window) with esbuild
// (CommonJS — a sandboxed preload can't be an ES module), the overlay's Vue app with Vite, and the
// pair window's plain page. Output in dist/.
import { build as esbuild } from 'esbuild'
import { build as vite } from 'vite'
import { copyFileSync, mkdirSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const dist = resolve(root, 'dist')
rmSync(dist, { recursive: true, force: true })

const common = {
  bundle: true,
  platform: 'node',
  format: 'cjs',
  target: 'node22',
  external: ['electron'],
  sourcemap: true,
  logLevel: 'warning',
}

await esbuild({ ...common, entryPoints: [resolve(root, 'src/main/main.ts')], outfile: resolve(dist, 'main/main.cjs') })
await esbuild({ ...common, entryPoints: [resolve(root, 'src/preload/overlay.ts')], outfile: resolve(dist, 'preload/overlay.cjs') })
await esbuild({ ...common, entryPoints: [resolve(root, 'src/preload/pair.ts')], outfile: resolve(dist, 'preload/pair.cjs') })
await esbuild({ ...common, entryPoints: [resolve(root, 'src/preload/prefs.ts')], outfile: resolve(dist, 'preload/prefs.cjs') })
await vite({ configFile: resolve(root, 'vite.overlay.config.ts'), logLevel: 'warn' })

// the pair window: a plain page, no framework
mkdirSync(resolve(dist, 'pair'), { recursive: true })
await esbuild({ bundle: true, platform: 'browser', format: 'iife', target: 'chrome140', sourcemap: true, logLevel: 'warning', entryPoints: [resolve(root, 'src/pair/pair.ts')], outfile: resolve(dist, 'pair/pair.js') })
for (const f of ['index.html', 'pair.css']) copyFileSync(resolve(root, 'src/pair', f), resolve(dist, 'pair', f))

// the settings window: a plain page too
mkdirSync(resolve(dist, 'prefs'), { recursive: true })
await esbuild({ bundle: true, platform: 'browser', format: 'iife', target: 'chrome140', sourcemap: true, logLevel: 'warning', entryPoints: [resolve(root, 'src/prefs/prefs.ts')], outfile: resolve(dist, 'prefs/prefs.js') })
for (const f of ['index.html', 'prefs.css']) copyFileSync(resolve(root, 'src/prefs', f), resolve(dist, 'prefs', f))
console.log('built', dist)
