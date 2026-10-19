// The shell's side of nemd: find or start the daemon, then talk to it over its private Unix
// socket (newline-delimited JSON, requests by id, pushed events). The device key never crosses
// this channel except once, at pairing, on its way into the OS keychain.
import { type ChildProcess, execFileSync, spawn } from 'node:child_process'
import { EventEmitter } from 'node:events'
import { createWriteStream, existsSync, mkdirSync } from 'node:fs'
import { createConnection, type Socket } from 'node:net'
import { join, resolve } from 'node:path'
import { app } from 'electron'

export interface NemdStatus {
  version: string
  base: string
  device: string
  paired: boolean
  companion: string
  key_uuid?: string
  presence: string
  socket: string
  reason?: string
  keychain?: string
}

export interface Companion { uuid: string, name: string }

export class NemdError extends Error {
  constructor(public code: string, message: string) { super(message) }
}

interface Waiter { resolve: (v: unknown) => void, reject: (e: Error) => void, timer: NodeJS.Timeout }

const sleep = (ms: number) => new Promise(r => setTimeout(r, ms))

/** Where the nemd binary is: next to the app when packaged, the repo's daemon/bin in development. */
export function nemdBinary(override?: string): string {
  if (override) return override
  const exe = process.platform === 'win32' ? 'nemd.exe' : 'nemd'
  if (app.isPackaged) return join(process.resourcesPath, exe)
  return resolve(app.getAppPath(), '..', '..', 'daemon', 'bin', exe)
}

export class Nemd extends EventEmitter {
  private sock: Socket | null = null
  private child: ChildProcess | null = null
  private seq = 0
  private waits = new Map<number, Waiter>()
  private buf = ''
  private stopping = false
  readonly socketPath: string

  /** profile: nemd's separate state ("dev" for development runs: its own config and socket) */
  constructor(private binary: string, private base?: string, private profile?: string) {
    super()
    if (!existsSync(binary)) throw new Error(`nemd not found at ${binary} (build it: cd daemon && go build -o bin/nemd ./cmd/nemd)`)
    this.socketPath = execFileSync(binary, ['--print-socket', ...this.profileArgs()]).toString().trim()
  }

  /** Connect to a running nemd, or start one and connect. */
  async start(): Promise<NemdStatus> {
    if (!(await this.connect())) {
      this.spawnDaemon()
      for (let i = 0; i < 60 && !this.sock; i++) {
        await sleep(100)
        await this.connect()
      }
      if (!this.sock) throw new Error('nemd did not start (see its log in the app data folder)')
    }
    return this.call<NemdStatus>('subscribe')
  }

  private spawnDaemon() {
    const logs = join(app.getPath('userData'), 'logs')
    mkdirSync(logs, { recursive: true })
    const out = createWriteStream(join(logs, 'nemd.log'), { flags: 'a' })
    const args = ['--socket', this.socketPath, ...this.profileArgs()]
    if (this.base) args.push('--base', this.base)
    const child = spawn(this.binary, args, { stdio: ['ignore', 'pipe', 'pipe'] })
    child.stdout?.pipe(out)
    child.stderr?.pipe(out)
    child.on('exit', (code) => {
      if (this.child === child) this.child = null
      if (!this.stopping) this.emit('daemon-exit', code)
    })
    this.child = child
  }

  private connect(): Promise<boolean> {
    return new Promise((done) => {
      const s = createConnection(this.socketPath)
      s.once('connect', () => {
        this.sock = s
        s.setEncoding('utf8')
        s.on('data', (chunk: string) => this.onData(chunk))
        s.on('close', () => this.onClose(s))
        done(true)
      })
      s.once('error', () => done(false))
    })
  }

  private onData(chunk: string) {
    this.buf += chunk
    let i: number
    while ((i = this.buf.indexOf('\n')) >= 0) {
      const line = this.buf.slice(0, i)
      this.buf = this.buf.slice(i + 1)
      if (!line.trim()) continue
      let msg: { id?: number, result?: unknown, error?: { code: string, message: string }, event?: string, data?: unknown }
      try { msg = JSON.parse(line) }
      catch { continue }
      if (msg.event) { this.emit('event', msg.event, msg.data); continue }
      const w = msg.id !== undefined ? this.waits.get(msg.id) : undefined
      if (!w) continue
      this.waits.delete(msg.id!)
      clearTimeout(w.timer)
      if (msg.error) w.reject(new NemdError(msg.error.code, msg.error.message))
      else w.resolve(msg.result)
    }
  }

  private onClose(s: Socket) {
    if (this.sock !== s) return
    this.sock = null
    for (const [id, w] of this.waits) {
      clearTimeout(w.timer)
      w.reject(new NemdError('disconnected', 'nemd connection closed'))
      this.waits.delete(id)
    }
    if (!this.stopping) this.emit('disconnected')
  }

  call<T = unknown>(method: string, params?: unknown, timeoutMs = 20_000): Promise<T> {
    const s = this.sock
    if (!s) return Promise.reject(new NemdError('disconnected', 'nemd is not connected'))
    const id = ++this.seq
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.waits.delete(id)
        reject(new NemdError('timeout', `${method}: no answer from nemd`))
      }, timeoutMs)
      this.waits.set(id, { resolve: v => resolve(v as T), reject, timer })
      s.write(JSON.stringify({ id, method, params }) + '\n')
    })
  }

  private profileArgs(): string[] { return this.profile ? ['--profile', this.profile] : [] }

  /** this shell started the running nemd (it's ours, the version this app ships) */
  get ours(): boolean { return !!this.child }

  /** Close the connection (after flushing what's queued); stop the daemon if this shell started it. */
  stop() {
    this.stopping = true
    this.sock?.end()
    this.child?.kill('SIGTERM')
  }
}
