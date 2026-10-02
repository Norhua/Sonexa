<script setup lang="ts">
import { computed } from 'vue'
import { ArrowLeft, RotateCcw, FileText } from 'lucide-vue-next'
import EditFieldInput from './EditFieldInput.vue'
import { editorFieldText, sameEditorValue } from '@/lib/editorDisplay'
import { valueOf, type EditValue } from '@/lib/useEditDraft'

const props = defineProps<{ field: string; label: string; value?: EditValue; upstream?: EditValue; changed: boolean }>()
defineEmits<{ change: [value: EditValue]; apply: []; undo: []; lyrics: [] }>()
const hasUpstream = computed(() => !!props.upstream && props.upstream.state !== 'unknown')
const equal = computed(() => hasUpstream.value && sameEditorValue(props.value, props.upstream))
const userValue = computed(() => !equal.value && (props.changed || props.value?.mode === 'user'))
</script>

<template>
  <div class="compare-field edit-grid" :class="{ 'is-changed': changed }">
    <div class="field-label">
      <span>{{ label }}</span>
      <span v-if="userValue" class="rounded bg-surface-100 px-1 text-[10px] font-normal text-muted-color dark:bg-surface-800">用户</span>
    </div>
    <div class="min-w-0">
      <button v-if="field === 'track.lyrics'" class="flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left text-sm hover:bg-emphasis" aria-label="编辑歌词" @click="$emit('lyrics')">
        <FileText class="size-4 shrink-0" />
        <span class="min-w-0 flex-1 truncate">{{ valueOf(value) ? '编辑歌词' : '添加歌词' }}</span>
        <span v-if="valueOf(value)" class="text-xs text-muted-color">{{ String(valueOf(value)).split('\n').length }} 行</span>
      </button>
      <EditFieldInput v-else :field="field" :label="label" :value="value" @change="$emit('change', $event)" />
    </div>
    <p class="min-w-0 whitespace-pre-wrap break-words pt-2 text-sm text-muted-color" :class="{ 'line-clamp-3': field === 'track.lyrics' }">{{ editorFieldText(upstream, field) }}</p>
    <div class="flex min-h-9 flex-wrap items-center justify-end gap-0.5">
      <span v-if="equal && !changed" class="whitespace-nowrap text-[11px] text-muted-color">= 相同</span>
      <button v-if="hasUpstream && !equal" class="field-action" :aria-label="`应用${label}`" :title="`应用上游${label}`" @click="$emit('apply')"><ArrowLeft class="size-4" /></button>
      <button v-if="changed" class="field-action" :aria-label="`撤销${label}`" :title="`撤销${label}修改`" @click="$emit('undo')"><RotateCcw class="size-4" /></button>
      <span v-if="!hasUpstream && !changed" class="pr-2 text-muted-color">—</span>
    </div>
  </div>
</template>

<style scoped>
.compare-field { position: relative; align-items: start; border-bottom: 1px solid var(--p-content-border-color); padding: 10px 0; }
.is-changed::before { content: ''; position: absolute; inset: 10px auto 10px -10px; width: 2px; border-radius: 2px; background: var(--p-primary-color); }
.field-label { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; grid-column: 1 / -1; font-size: 12px; font-weight: 500; }
.field-action { display: inline-flex; align-items: center; justify-content: center; width: 26px; height: 30px; border-radius: 4px; cursor: pointer; }
.field-action:hover { background: var(--p-content-hover-background); }
@container editor-fields (min-width: 600px) { .field-label { grid-column: auto; padding-top: 9px; } }
</style>
