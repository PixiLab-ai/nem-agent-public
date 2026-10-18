<script setup lang="ts">
// The pet's card on the desktop (a right-click on the pet): who it is — name or species, rarity,
// its four stats, how often it was petted, when it hatched, whose it is. The web client's card
// (MePet.vue). Words come through `t` (the shell's translator: the web's own translations for
// species, rarities and stats); without one, English.
import { computed } from 'vue'
import type { Pet } from './usePet'
import { PET_SPRITES, petCells, type PetSpecies } from './petSprites'

const props = defineProps<{ pet: Pet, owner?: string, t?: (key: string, vars?: Record<string, string | number>) => string }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const STATS = ['curiosity', 'chaos', 'nap', 'snack'] as const
const species = computed<PetSpecies>(() => (PET_SPRITES[props.pet.species as PetSpecies] ? props.pet.species : 'blob') as PetSpecies)
const cells = computed(() => petCells(PET_SPRITES[species.value]))
/** a translated string, or the English given when there's no translator */
function tr(key: string, english: string, vars?: Record<string, string | number>): string {
  return props.t ? props.t(key, vars) : english.replace(/\{\{\s*(\w+)\s*\}\}/g, (m, k: string) => (vars && k in vars ? String(vars[k]) : m))
}
const speciesName = computed(() => tr(`pet.species.${props.pet.species}`, props.pet.species))
const title = computed(() => (props.pet.name || '').trim() || cap(speciesName.value))
const rarity = computed(() => props.pet.rarity || 'common')
const hatched = computed(() => {
  try { return new Intl.DateTimeFormat(undefined, { day: 'numeric', month: 'short', year: 'numeric' }).format(new Date(props.pet.hatched_at)) }
  catch { return '' }
})
const pets = computed(() => props.pet.pets === 1
  ? tr('pet.pets_1', 'Petted once')
  : props.pet.pets ? tr('pet.pets_n', 'Petted {{n}} times', { n: props.pet.pets }) : tr('pet.pets_0', 'Never petted yet'))
// (built here, not in the template: a "{{…}}" inside a template expression would end it)
const hatchedText = computed(() => hatched.value ? tr('pet.hatched', 'hatched {{when}}', { when: hatched.value }) : '')
const whose = computed(() => props.owner ? tr('pet.whose', "{{name}}'s pet", { name: props.owner }) : '')
function cap(s: string) { return s ? s[0].toUpperCase() + s.slice(1) : s }
</script>

<template>
  <div class="np-card" data-hit role="dialog" :aria-label="title">
    <div class="np-card-top">
      <svg class="np-card-sprite" viewBox="0 0 16 16" shape-rendering="crispEdges" aria-hidden="true">
        <rect v-for="c in cells" :key="`${c.x}-${c.y}`" :x="c.x" :y="c.y" width="1" height="1" :fill="c.c" />
      </svg>
      <div class="np-card-id">
        <b>{{ title }}</b>
        <span class="np-chip" :class="`r-${rarity}`">{{ tr(`pet.rarity.${rarity}`, rarity) }} · {{ speciesName }}</span>
      </div>
      <button type="button" class="np-card-x" :aria-label="tr('pet.close', 'Close')" @click="emit('close')">×</button>
    </div>
    <ul class="np-stats">
      <li v-for="k in STATS" :key="k">
        <span>{{ tr(`pet.stat.${k}`, k) }}</span>
        <i aria-hidden="true"><b :style="{ width: `${(pet.stats?.[k] || 0) * 10}%` }" /></i>
        <span class="np-num">{{ pet.stats?.[k] || 0 }}</span>
      </li>
    </ul>
    <p class="np-card-foot">{{ pets }}<template v-if="hatchedText"> · {{ hatchedText }}</template><template v-if="whose"> · {{ whose }}</template></p>
  </div>
</template>
