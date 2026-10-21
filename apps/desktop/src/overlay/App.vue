<script setup lang="ts">
// The overlay page: the pet, fed by the main process (pet info, chat frames, demo steps), and
// the plumbing that keeps the window click-through except over the pet (see main/overlay.ts).
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { type Pet, PetCard, type PetFrame, type PetInfo, PetSprite, usePet } from '@nem-agent/pet'
import { pickLang, translator } from '../shared/i18n'

const query = new URLSearchParams(location.search)
const mode = query.get('mode') === 'forward' ? 'forward' : 'shape'
/** the shell's language (main sends it), for the card */
const t = translator(pickLang(query.get('lang')))
const { pet, bubble, hatching, hearts, say, burst, setInfo, onFrame } = usePet()
const sprite = ref<InstanceType<typeof PetSprite> | null>(null)
const thinking = ref(false)
const mood = ref('')
let moodTimer: ReturnType<typeof setTimeout> | null = null
/** the pet's card (a right-click on the pet), and whose pet it is (her name, from the main process) */
const card = ref(false)
const owner = ref('')
/** Peek is on: the pet shows it's noticing what you're up to (never hidden, never secret) */
const peeking = ref(false)
let cardTimer: ReturnType<typeof setTimeout> | null = null
let dragging = false
let hovering = false
const offs: (() => void)[] = []

// --- what the main process sends ------------------------------------------------------------

interface ChatFrame { type: string, face?: string, kind?: PetFrame['kind'], text?: string, trigger?: string, pet?: Pet }

function onChat(f: ChatFrame) {
  switch (f.type) {
    case 'turn_start':
      thinking.value = true
      break
    case 'turn_end':
    case 'turn_idle':
      thinking.value = false
      break
    case 'mood':
      mood.value = f.face || ''
      if (moodTimer) clearTimeout(moodTimer)
      moodTimer = setTimeout(() => { mood.value = '' }, 8000)
      break
    case 'pet':
      if (f.kind) onFrame({ kind: f.kind, text: f.text, trigger: f.trigger, pet: f.pet })
      break
  }
}

// --- taps and drags -------------------------------------------------------------------------

async function doTap() {
  if (hatching.value || !pet.value) return
  burst(3)
  sprite.value?.hop()
  try {
    const r = await window.nem.tap() as { pet?: Pet, line?: string } | null
    if (r?.pet) pet.value = r.pet
    if (r?.line) say(r.line)
  }
  catch { /* the tap still happened on screen */ }
}

/** A right-click opens or closes the card; it closes by itself after a while (the window can't take focus, so no Escape). */
function onMenu() {
  card.value = !card.value
  if (cardTimer) clearTimeout(cardTimer)
  if (card.value) cardTimer = setTimeout(() => { card.value = false }, 20_000)
}

/** Clicks from a pointer are handled at button-up (a drag isn't a tap); this is keyboard activation. */
function onTap(ev: MouseEvent) {
  if (ev.detail === 0) void doTap()
}

async function onGrab(ev: PointerEvent) {
  if (ev.button !== 0) return
  ev.preventDefault()
  const el = ev.currentTarget as HTMLElement
  el.setPointerCapture(ev.pointerId)
  dragging = true
  const started = window.nem.grabStart()
  const end = async (up: PointerEvent) => {
    el.removeEventListener('pointerup', end)
    el.removeEventListener('pointercancel', end)
    await started
    const { moved } = await window.nem.grabEnd()
    dragging = false
    if (!moved && up.type === 'pointerup') void doTap()
  }
  el.addEventListener('pointerup', end)
  el.addEventListener('pointercancel', end)
}

// --- click-through --------------------------------------------------------------------------

/** Is this element part of the pet? On the sprite, only its painted pixels count (the cells are SVG rects). */
function isHit(el: Element | null): boolean {
  const h = el?.closest<HTMLElement>('[data-hit]')
  if (!h) return false
  return h.dataset.hit !== 'alpha' || el!.tagName.toLowerCase() === 'rect'
}

// shape mode: report the pet's boxes whenever they may have changed. On X11 the shape clips what
// is drawn as well as the clicks, so the boxes also cover the pet's effects: shadows, the halo,
// the hop (a 10 px margin) and, while a tap's hearts are flying, the air above the sprite.
const PAD = 10
const HEARTS_UP = 56
let hitsQueued = false
function reportHits() {
  if (mode !== 'shape' || hitsQueued) return
  hitsQueued = true
  requestAnimationFrame(() => {
    hitsQueued = false
    // [data-shape]: drawn, not clickable (the Peek eyes sit past the sprite's padded box; X11 would clip them)
    const rects = [...document.querySelectorAll<HTMLElement>('[data-hit], [data-shape]')].map((el) => {
      const r = el.getBoundingClientRect()
      return { x: r.left - PAD, y: r.top - PAD, width: r.width + PAD * 2, height: r.height + PAD * 2 }
    })
    const btn = document.querySelector<HTMLElement>('[data-hit="alpha"]')
    if (btn && hearts.value.length) {
      const r = btn.getBoundingClientRect()
      rects.push({ x: r.left - 24, y: r.top - HEARTS_UP, width: r.width + 48, height: HEARTS_UP })
    }
    window.nem.hits(rects)
  })
}

// forward mode: turn the mouse back on only over the pet
function onMove(ev: MouseEvent) {
  if (mode !== 'forward' || dragging) return
  const over = isHit(document.elementFromPoint(ev.clientX, ev.clientY))
  if (over !== hovering) {
    hovering = over
    window.nem.hover(over)
  }
}
function onLeave() {
  if (mode !== 'forward' || dragging || !hovering) return
  hovering = false
  window.nem.hover(false)
}

watch([pet, bubble, thinking, mood, hatching, card, peeking, () => hearts.value.length > 0], () => nextTick(reportHits))

onMounted(() => {
  offs.push(window.nem.on('overlay:info', (p) => {
    setInfo(p as PetInfo)
    owner.value = (p as { owner?: string }).owner || owner.value
  }))
  offs.push(window.nem.on('overlay:frame', f => onChat(f as ChatFrame)))
  offs.push(window.nem.on('overlay:say', t => say(String(t))))
  offs.push(window.nem.on('overlay:peek', p => { peeking.value = !!(p as { on?: boolean }).on }))
  document.addEventListener('transitionend', reportHits)
  document.addEventListener('animationend', reportHits)
  document.addEventListener('mousemove', onMove)
  document.documentElement.addEventListener('mouseleave', onLeave)
  window.addEventListener('resize', reportHits)
  reportHits()
})
onBeforeUnmount(() => {
  offs.forEach(off => off())
  if (moodTimer) clearTimeout(moodTimer)
  if (cardTimer) clearTimeout(cardTimer)
})
</script>

<template>
  <div class="stage">
    <div class="column">
      <PetCard v-if="card && pet && !pet.muted" :pet="pet" :owner="owner" :t="t" @close="card = false" />
      <!-- the eyes go in the sprite's own box: beside the pet, not at the top of the bubble above it (a tester, 2026-10-21) -->
      <PetSprite ref="sprite" :scale="4" :thinking="thinking" :mood="mood" :quiet="card" @tap="onTap" @grab="onGrab" @menu="onMenu">
        <template #side>
          <span v-if="peeking && pet && !pet.muted" class="peek-eyes" data-shape aria-hidden="true">👀</span>
        </template>
      </PetSprite>
    </div>
  </div>
</template>
