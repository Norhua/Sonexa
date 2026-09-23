<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ImageOff, LoaderCircle } from 'lucide-vue-next'
import { coverUrl, Events, EventsOn, type CoverSource } from '@/api'
import { coverOutcome } from '@/lib/coverStatus'

const props = withDefaults(defineProps<{
  source?: CoverSource | null
  usage: 'thumbnail' | 'detail'
  iconClass?: string
  labelClass?: string
}>(), { iconClass: 'size-4', labelClass: 'text-[10px]' })
const eventRevision = ref(0)
watch(() => [props.source?.releaseId, props.source?.provider, props.source?.ref], () => { eventRevision.value = 0 })
const effectiveSource = computed(() => props.source ? { ...props.source, revision: Math.max(props.source.revision ?? 0, eventRevision.value) } : null)
const off = EventsOn(Events.CoverChanged, (event: { releaseId: number; revision: number; provider?: string; ref?: string }) => {
  const source = props.source
  const matches = source?.releaseId ? event.releaseId === source.releaseId
    : !!source?.provider && !!source.ref && source.provider === event.provider && source.ref === event.ref
  if (matches && event.revision > (effectiveSource.value?.revision ?? 0)) eventRevision.value = event.revision
})
onBeforeUnmount(off)
type State = 'loading' | 'ok' | 'empty' | 'error' | 'disabled'
const state = ref<State>('empty')
const src = computed(() => coverUrl(effectiveSource.value, props.usage))
watch(src, url => { state.value = url ? 'loading' : 'empty' }, { immediate: true })
function onLoad(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  if (image.getAttribute('src') === src.value) state.value = 'ok'
}
async function onError(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  if (image.getAttribute('src') !== src.value) return
  const current = src.value
  try {
    const result = await coverOutcome(effectiveSource.value ?? {}, props.usage, effectiveSource.value?.revision ?? 0)
    if (src.value !== current) return
    state.value = result
  } catch { if (src.value === current) state.value = 'error' }
}
</script>

<template>
  <div class="relative flex items-center justify-center overflow-hidden bg-surface-100 text-muted-color dark:bg-surface-800">
    <img v-if="src" :key="src" :src="src" loading="lazy" alt="" class="absolute inset-0 size-full object-cover"
      :class="{ 'opacity-0': state !== 'ok' }" @load="onLoad" @error="onError" />
    <LoaderCircle v-if="state === 'loading'" :class="[iconClass, 'animate-spin']" />
    <ImageOff v-else-if="state === 'error'" :class="[iconClass, 'text-danger-color']" title="封面加载失败" />
    <span v-else-if="state === 'disabled'" :class="labelClass" title="出网已禁用，请在设置中填写联系方式">出网已禁用</span>
    <span v-else-if="state === 'empty'" :class="[labelClass, 'font-mono leading-none opacity-60']">nil</span>
  </div>
</template>
