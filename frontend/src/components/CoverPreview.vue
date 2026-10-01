<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ImageOff, LoaderCircle } from 'lucide-vue-next'
import type { Models } from '@/api'

const props = defineProps<{ variant: Models.domain.CoverVariant; large?: boolean }>()
const emit = defineEmits<{ verified: [id: number, width: number, height: number] }>()
const src = computed(() => `/cover?variantId=${props.variant.id}`)
const state = ref<'loading' | 'ok' | 'error'>('loading')
watch(src, () => { state.value = 'loading' })
function loaded(event: Event) {
  const image = event.currentTarget as HTMLImageElement
  if (image.getAttribute('src') !== src.value) return
  state.value = 'ok'
  emit('verified', props.variant.id, image.naturalWidth, image.naturalHeight)
}
function failed(event: Event) {
  if ((event.currentTarget as HTMLImageElement).getAttribute('src') === src.value) state.value = 'error'
}
</script>

<template>
  <div class="relative flex items-center justify-center" :class="large ? 'h-[65vh]' : 'size-full'">
    <img :key="src" :src="src" :alt="large ? '封面大图' : '封面预览'" loading="lazy"
      class="absolute inset-0 size-full object-contain" :class="{ 'opacity-0': state !== 'ok' }"
      @load="loaded" @error="failed" />
    <LoaderCircle v-if="state === 'loading'" class="size-6 animate-spin text-muted-color" aria-label="预览加载中" />
    <span v-else-if="state === 'error'" class="flex items-center gap-2 text-sm text-danger-color" role="status">
      <ImageOff class="size-5" />预览失败，请尝试其他规格或重试来源
    </span>
  </div>
</template>
