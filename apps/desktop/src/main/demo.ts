// Demo mode (`--demo`): the pet's states on a loop, with no account, no daemon and no network.
// For testing the overlay on a new desktop (spike A): the shape, staying on top, the drag, the
// bubble, the hatch, and the idle CPU between steps.
import { PET_SPECIES } from '@nem-agent/pet/sprites'
import type { Overlay } from './overlay'

const LINES = [
  'hi! I live on your desktop now.',
  'you\'ve been typing for a while. water?',
  'she says she misses you. I\'m just the messenger.',
  'drag me anywhere. I don\'t bite. much.',
  'this bubble stays a little longer when there\'s more to read, so nobody misses the end of a long thought.',
]
const TAP_LINES = ['hehe', 'again!', '♥', 'that tickles']
const RARITIES = ['common', 'uncommon', 'rare', 'epic', 'legendary']

function demoPet(i: number) {
  return {
    companion_uuid: 'demo',
    species: PET_SPECIES[i % PET_SPECIES.length],
    rarity: RARITIES[i % RARITIES.length],
    name: '',
    stats: { curiosity: 7, chaos: 4, nap: 6, snack: 9 },
    hatched_at: new Date().toISOString(),
    pets: 0,
    last_pet_at: null,
    muted: false,
  }
}

export class Demo {
  private i = 0
  private step = 0
  private timer: NodeJS.Timeout | null = null
  private pets = 0

  /** still: one hello, then nothing — for measuring what an idle pet costs */
  constructor(private overlay: Overlay, private still = false) {}

  start() {
    this.overlay.win.webContents.once('did-finish-load', () => {
      this.overlay.send('overlay:info', { pet: demoPet(this.i), egg: false })
      this.overlay.setVisible(true)
      this.timer = setTimeout(() => this.still ? this.hello() : this.next(), 1500)
    })
  }

  private hello() {
    this.overlay.send('overlay:frame', { type: 'pet', kind: 'say', text: LINES[0], trigger: 'arrived' })
  }

  /** A tap in demo mode answers like the site does: the count moves, sometimes a line back. */
  tap() {
    this.pets++
    const pet = { ...demoPet(this.i), pets: this.pets }
    return { pet, line: this.pets % 2 ? TAP_LINES[this.pets % TAP_LINES.length] : undefined }
  }

  private next() {
    const say = (text: string) => this.overlay.send('overlay:frame', { type: 'pet', kind: 'say', text, trigger: 'idle' })
    const frame = (f: Record<string, unknown>) => this.overlay.send('overlay:frame', f)
    let wait = 9000
    switch (this.step % 6) {
      case 0: say(LINES[(this.step / 6 | 0) % LINES.length]); break
      case 1: frame({ type: 'turn_start' }); wait = 3500; break
      case 2: frame({ type: 'mood', face: ['happy', 'blushing', 'smug', 'surprised'][this.i % 4] }); frame({ type: 'turn_end', reply: '' }); break
      case 3: say(LINES[4]); wait = 14000; break
      case 4: {
        this.i++
        frame({ type: 'pet', kind: 'hatch', pet: demoPet(this.i) })
        wait = 6000
        break
      }
      case 5: say(`a ${PET_SPECIES[this.i % PET_SPECIES.length]} now. ${RARITIES[this.i % RARITIES.length]}, apparently.`); break
    }
    this.step++
    this.timer = setTimeout(() => this.next(), wait)
  }

  stop() {
    if (this.timer) clearTimeout(this.timer)
    this.timer = null
  }
}
