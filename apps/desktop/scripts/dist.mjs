// Installers for one OS: nemd built for it (static, no cgo), the app bundle, then electron-builder.
//   node scripts/dist.mjs linux | mac | win  [--arch=x64|arm64] [--publish]
// Default arches: x64 on Linux and Windows, both on macOS. Output in out/. --publish uploads the
// installers and latest*.yml to the draft release of this version on GitHub (electron-builder.yml
// publish; needs GH_TOKEN) — what the release workflow does; local builds never publish.
import { execFileSync } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const repo = resolve(root, '..', '..')
const os = process.argv[2]
const GOOS = { linux: 'linux', mac: 'darwin', win: 'windows' }[os]
if (!GOOS) {
  console.error('usage: node scripts/dist.mjs linux|mac|win [--arch=x64|arm64]')
  process.exit(2)
}
const archArg = process.argv.find(a => a.startsWith('--arch='))?.slice(7)
const arches = archArg ? [archArg] : os === 'mac' ? ['arm64', 'x64'] : ['x64']
const GOARCH = { x64: 'amd64', arm64: 'arm64' }
const { version } = JSON.parse(readFileSync(resolve(root, 'package.json'), 'utf8'))
// Optional machine-local settings (gitignored): KEY=VALUE lines at the repo root, e.g. where the
// Go, Electron and electron-builder caches live. The environment wins over the file.
const caches = {}
const local = resolve(repo, 'local.env')
if (existsSync(local)) {
  for (const line of readFileSync(local, 'utf8').split('\n')) {
    const m = line.match(/^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*?)\s*$/)
    if (m && !line.trimStart().startsWith('#') && process.env[m[1]] === undefined) caches[m[1]] = m[2]
  }
}

for (const a of arches) {
  if (!GOARCH[a]) throw new Error(`unknown arch ${a}`)
  const dir = resolve(repo, 'daemon', 'bin', `${GOOS === 'darwin' ? 'darwin' : GOOS}-${a}`)
  mkdirSync(dir, { recursive: true })
  console.log(`nemd ${version} for ${GOOS}/${GOARCH[a]}`)
  execFileSync('go', ['build', '-trimpath', '-ldflags', `-s -w -X main.version=${version}`, '-o', resolve(dir, os === 'win' ? 'nemd.exe' : 'nemd'), './cmd/nemd'], {
    cwd: resolve(repo, 'daemon'),
    stdio: 'inherit',
    env: { ...process.env, ...caches, GOOS, GOARCH: GOARCH[a], CGO_ENABLED: '0' },
  })
}

execFileSync('node', ['scripts/build.mjs'], { cwd: root, stdio: 'inherit' })
const publish = process.argv.includes('--publish') ? 'always' : 'never'
// electron-builder's CLI through node itself: its .bin shim is a .cmd on Windows, which Node
// doesn't start without a shell
execFileSync(process.execPath, [resolve(root, 'node_modules', 'electron-builder', 'cli.js'), `--${os}`, ...arches.map(a => `--${a}`), '--config', 'electron-builder.yml', '--publish', publish], {
  cwd: root,
  stdio: 'inherit',
  env: { ...process.env, ...caches },
})
