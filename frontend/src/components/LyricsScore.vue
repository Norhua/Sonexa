<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, useId } from 'vue'
import { lyricMaxScore, lyricScoreDescription, type LyricCandidate } from '@/lib/lyricsPresentation'
const props = defineProps<{ candidate: LyricCandidate }>()
const id = useId()
const trigger = ref<HTMLElement>(), tooltip = ref<HTMLElement>()
const visible = ref(false)
const position = ref({ left: '0px', top: '0px', zIndex: 1100 })
const description = computed(() => lyricScoreDescription(props.candidate))
let hideTimer: ReturnType<typeof setTimeout> | undefined
function hide() {
  clearTimeout(hideTimer)
  visible.value = false
  document.removeEventListener('keydown', onEscape, true)
  window.removeEventListener('scroll', hide, true)
  window.removeEventListener('resize', hide)
}
function onEscape(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  event.preventDefault(); event.stopPropagation(); hide()
}
function keep() { clearTimeout(hideTimer) }
function leave() { hideTimer = setTimeout(hide, 100) }
async function show() {
  keep()
  if (visible.value || !trigger.value) return
  const anchor = trigger.value.getBoundingClientRect()
  let zIndex = 1100
  for (let el: HTMLElement | null = trigger.value; el; el = el.parentElement) {
    zIndex = Math.max(zIndex, (Number.parseInt(getComputedStyle(el).zIndex) || 0) + 1)
  }
  position.value = { left: `${anchor.left}px`, top: `${anchor.bottom}px`, zIndex }
  visible.value = true
  await nextTick()
  if (!visible.value || !tooltip.value) return
  const box = tooltip.value.getBoundingClientRect()
  position.value.left = `${Math.max(8, Math.min(anchor.right - box.width, window.innerWidth - box.width - 8))}px`
  position.value.top = `${Math.max(8, anchor.bottom + box.height + 4 > window.innerHeight - 8 ? anchor.top - box.height - 4 : anchor.bottom + 4)}px`
  document.addEventListener('keydown', onEscape, true)
  window.addEventListener('scroll', hide, true)
  window.addEventListener('resize', hide)
}
onBeforeUnmount(hide)
</script>

<template>
  <button ref="trigger" type="button" class="absolute top-3 right-3 cursor-help rounded bg-emphasis px-2 py-1 text-xs tabular-nums focus-visible:outline focus-visible:outline-primary"
    :aria-label="`综合得分 ${candidate.totalScore ?? '—'}/${lyricMaxScore}`" :aria-describedby="id"
    @mouseenter="show" @mouseleave="leave" @focus="show" @blur="hide" @click="show">
    {{ candidate.totalScore ?? '—' }}/{{ lyricMaxScore }}
  </button>
  <Teleport to="body">
    <div v-show="visible" :id="id" ref="tooltip" role="tooltip" :style="position" class="fixed w-64 max-w-[calc(100vw-1rem)] rounded border bg-surface-0 p-3 text-xs leading-relaxed text-color shadow-lg dark:bg-surface-900" @mouseenter="keep" @mouseleave="leave">{{ description }}</div>
  </Teleport>
</template>
