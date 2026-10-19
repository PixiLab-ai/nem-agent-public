<script setup lang="ts">
// The pet on the desktop: a 16×16 pixel sprite that blinks on its own, hops when it speaks or
// is tapped, bursts hearts on a tap, plays the egg when it hatches, and shows her mood as a
// small badge while she's writing or just replied. Every motion is a CSS animation on the
// compositor — no requestAnimationFrame loop, so it costs nothing while nothing happens.
// Adapted from the Nemini web client's MePet.vue (the card and its i18n stay on the site).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { usePet } from './usePet'
import { PET_BLINK, PET_EGG, PET_SPRITES, petCells, type PetSpecies } from './petSprites'

const props = defineProps<{
  /** px per sprite pixel (integer scaling keeps the pixels crisp) */
  scale?: number
  /** she's writing right now */
  thinking?: boolean
  /** the face of her last reply (`mood` frame), shown briefly as a badge */
  mood?: string
  /** something sits above the pet (its card): no bubble meanwhile */
  quiet?: boolean
}>()
const emit = defineEmits<{ (e: 'tap', ev: MouseEvent): void, (e: 'grab', ev: PointerEvent): void, (e: 'menu', ev: MouseEvent): void }>()

const { pet, egg, bubble, spoke, hatching, hearts, hush } = usePet()
const blink = ref(false)
const happy = ref(false)
let blinkTimer: ReturnType<typeof setTimeout> | null = null
let blinkBack: ReturnType<typeof setTimeout> | null = null
let alive = true

const size = computed(() => 16 * (props.scale || 4))
const species = computed<PetSpecies | null>(() => {
  const s = pet.value?.species as PetSpecies | undefined
  return s && PET_SPRITES[s] ? s : null
})
const cells = computed(() => {
  if (hatching.value || !species.value) return petCells(PET_EGG)
  return petCells(PET_SPRITES[species.value], blink.value ? PET_BLINK[species.value] : undefined)
})
const rarity = computed(() => pet.value?.rarity || 'common')
const visible = computed(() => !!pet.value && !pet.value.muted && !egg.value)
const moodIcon = computed(() => ({
  happy: '♪', sad: '…', angry: '!', surprised: '!?', blushing: '♡', smug: '~', thinking: '…', asleep: 'z',
  // the desktop app's Peek (what the human is up to): hard at work, playing
  focused: '✎', gaming: '★',
} as Record<string, string>)[props.mood || ''] || '')

function scheduleBlink() {
  if (!alive) return
  blinkTimer = setTimeout(() => {
    if (!alive) return
    blink.value = true
    blinkBack = setTimeout(() => { blink.value = false; scheduleBlink() }, 140)
  }, 3000 + Math.random() * 3000)
}

function hop() {
  happy.value = true
  setTimeout(() => { happy.value = false }, 650)
}

defineExpose({ hop })
watch(spoke, hop)
onMounted(scheduleBlink)
onBeforeUnmount(() => { alive = false; if (blinkTimer) clearTimeout(blinkTimer); if (blinkBack) clearTimeout(blinkBack) })
</script>

<template>
  <div v-if="visible" class="np" :class="[`r-${rarity}`, { happy, hatching, thinking }]">
    <Transition name="np-bubble">
      <div v-if="bubble && !quiet" class="np-bubble" data-hit role="status" aria-live="polite" @click="hush">{{ bubble }}</div>
    </Transition>
    <div class="np-wrap">
      <button
        type="button" class="np-btn" data-hit="alpha" :style="{ width: `${size}px`, height: `${size}px` }"
        :aria-label="pet?.name || pet?.species"
        @pointerdown="emit('grab', $event)" @click="emit('tap', $event)" @contextmenu.prevent="emit('menu', $event)"
      >
        <svg viewBox="0 0 16 16" shape-rendering="crispEdges" aria-hidden="true">
          <rect v-for="c in cells" :key="`${c.x}-${c.y}`" :x="c.x" :y="c.y" width="1" height="1" :fill="c.c" />
        </svg>
        <span v-for="h in hearts" :key="h" class="np-heart" :style="{ '--dx': `${((h % 5) - 2) * 7}px`, '--d': `${(h % 3) * 90}ms` }" aria-hidden="true">♥</span>
      </button>
      <span v-if="thinking" class="np-badge np-dots" aria-hidden="true"><i /><i /><i /></span>
      <span v-else-if="moodIcon" class="np-badge" aria-hidden="true">{{ moodIcon }}</span>
    </div>
  </div>
</template>
