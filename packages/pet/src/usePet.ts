// The pet's moment-to-moment state on the desktop. Adapted from the Nemini web client's
// usePet: the bubble is a moment (never stored), a tap bursts hearts, a hatch plays the egg.
// What the pet *is* (species, rarity, muted) comes from the API through the daemon.
import { ref } from 'vue'

/** The pet as the API sends it (`GET …/pet`, `POST …/pet/pet`, a `pet` frame). */
export interface Pet {
  companion_uuid: string
  species: string
  rarity: string
  name: string
  stats: Record<string, number>
  hatched_at: string
  pets: number
  last_pet_at: string | null
  /** the human's account-wide switch: hidden and quiet everywhere */
  muted: boolean
}

/** `GET …/pet`: the pet, or the egg (not hatched yet — nothing shows until the human hatches it). */
export interface PetInfo { pet: Pet | null, egg: boolean }

/** A `pet` frame on the chat socket. */
export interface PetFrame { kind: 'say' | 'hatch' | 'update', text?: string, trigger?: string, pet?: Pet }

const pet = ref<Pet | null>(null)
const egg = ref(false)
const bubble = ref('')
const hatching = ref(false)
const hearts = ref<number[]>([])
/** bumps on every line, so the sprite hops toward its bubble */
const spoke = ref(0)
let bubbleTimer: ReturnType<typeof setTimeout> | null = null
let heartSeq = 0

/** A bubble stays 7 s plus a beat per character, 18 s at most (the web client's timing). */
export function bubbleMs(text: string): number { return Math.min(18_000, 7000 + text.length * 80) }

function say(text: string) {
  const s = (text || '').trim()
  if (!s) return
  bubble.value = s
  spoke.value++
  if (bubbleTimer) clearTimeout(bubbleTimer)
  bubbleTimer = setTimeout(() => { bubble.value = '' }, bubbleMs(s))
}

/** Clears the bubble now (the human clicked it away). */
function hush() {
  if (bubbleTimer) { clearTimeout(bubbleTimer); bubbleTimer = null }
  bubble.value = ''
}

function burst(n = 3) {
  for (let i = 0; i < n; i++) {
    const id = ++heartSeq
    hearts.value.push(id)
    setTimeout(() => { hearts.value = hearts.value.filter(h => h !== id) }, 1400 + i * 120)
  }
}

function setInfo(info: PetInfo) {
  pet.value = info.pet
  egg.value = !info.pet && info.egg
}

function setPet(p: Pet, hatchedNow = false) {
  const fromEgg = egg.value || !pet.value
  pet.value = p
  egg.value = false
  if (hatchedNow || fromEgg) {
    hatching.value = true
    setTimeout(() => { hatching.value = false }, 2200)
  }
}

function onFrame(f: PetFrame) {
  if (f.kind === 'hatch') {
    if (f.pet) setPet(f.pet, true)
    return
  }
  if (f.kind === 'update') {
    if (f.pet) {
      pet.value = f.pet
      if (f.pet.muted) bubble.value = ''
    }
    return
  }
  if (f.text) say(f.text)
}

export function usePet() {
  return { pet, egg, bubble, spoke, hatching, hearts, say, hush, burst, setInfo, setPet, onFrame }
}
