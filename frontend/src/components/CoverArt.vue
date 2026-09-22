<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { ImageOff, LoaderCircle } from 'lucide-vue-next'
import { App, coverUrl, Events, EventsOn, type CoverSource } from '@/api'

const props = withDefaults(defineProps<{
  source?: CoverSource | null
  adoptedPath?: string | null
  size?: number
  usage?: 'thumbnail' | 'detail'
  iconClass?: string
  labelClass?: string
}>(), { usage: 'thumbnail', iconClass: 'size-4', labelClass: 'text-[10px]' })
const refresh = ref(0)
const off = EventsOn(Events.CoverChanged, () => { refresh.value++ })
onBeforeUnmount(off)
type State = 'loading' | 'ok' | 'empty' | 'error' | 'disabled'
const state = ref<State>('empty')
const src = computed(() => {
  const url = coverUrl(props.source, props.usage)
  return url ? `${url}&refresh=${refresh.value}` : ''
})
watch(src, url => { state.value = url ? 'loading' : 'empty' }, { immediate: true })
async function onError() {
  const current = src.value
  try {
    const result = await App.CoverTargetOutcomes([{
      releaseId: props.source?.releaseId ?? 0,
      provider: props.source?.provider ?? '', ref: props.source?.ref ?? '',
      groupRef: props.source?.rg ?? '', usage: props.usage,
    }])
    if (src.value !== current) return
    state.value = result[0] === 'empty' ? 'empty' : result[0] === 'disabled' ? 'disabled' : 'error'
  } catch { if (src.value === current) state.value = 'error' }
}
</script>

<template>
  <div class="relative flex items-center justify-center overflow-hidden bg-surface-100 text-muted-color dark:bg-surface-800">
    <img v-if="src" :key="src" :src="src" loading="lazy" alt="" class="absolute inset-0 size-full object-cover"
      :class="{ 'opacity-0': state !== 'ok' }" @load="state = 'ok'" @error="onError" />
    <LoaderCircle v-if="state === 'loading'" :class="[iconClass, 'animate-spin']" />
    <ImageOff v-else-if="state === 'error'" :class="[iconClass, 'text-danger-color']" title="封面加载失败" />
    <span v-else-if="state === 'disabled'" :class="labelClass" title="出网已禁用，请在设置中填写联系方式">出网已禁用</span>
    <span v-else-if="state === 'empty'" :class="[labelClass, 'font-mono leading-none opacity-60']">nil</span>
  </div>
</template>
