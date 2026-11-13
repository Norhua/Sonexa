<script setup lang="ts">
import { computed } from 'vue'
import { candidateReleaseState } from '@/lib/candidateReleaseState'

const props = defineProps<{
  album?: string | null
  releaseExists?: boolean
  releaseGroupExists?: boolean
  /** 保留所在列表的原有空值文案。 */
  fallback?: string
  /** 专辑类型等原有全文提示，不参与存在性判断。 */
  detail?: string
}>()
const state = computed(() => candidateReleaseState(props))
const title = computed(() => [props.album || props.fallback, props.detail, state.value.label, state.value.description].filter(Boolean).join(' · '))
</script>

<template>
  <span :class="state.className" :title="title">
    {{ album || fallback }}
    <span v-if="state.label" class="sr-only">（{{ state.label }}；{{ state.description }}）</span>
  </span>
</template>
