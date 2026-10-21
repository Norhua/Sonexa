<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { lyricReadingRows, lyricTime, type LyricDocument, type LyricExtraChannel } from '@/lib/lyricsPresentation'
import SecondaryButton from '@/volt/SecondaryButton.vue'
const props = defineProps<{ document: LyricDocument; candidateId: string; version: number; extraChannel: LyricExtraChannel }>()
const emit = defineEmits<{ displayed: [id: string, version: number] }>()
const mode = ref<'reading' | 'raw'>('reading')
const rows = computed(() => lyricReadingRows(props.document.rows, props.extraChannel))
watch(() => [props.candidateId, props.version, props.document] as const, async ([id, version, doc], _, onCleanup) => {
  let valid = true
  onCleanup(() => { valid = false })
  mode.value = 'reading'
  await nextTick()
  if (valid && doc.text && doc.rows.length) emit('displayed', id, version)
}, { immediate: true, flush: 'post' })
</script>

<template>
  <section aria-label="歌词正文">
    <div class="mb-4 flex flex-wrap items-center gap-3" aria-label="歌词预览工具栏">
      <div class="flex gap-2" role="group" aria-label="预览模式">
        <SecondaryButton size="small" :aria-pressed="mode === 'reading'" @click="mode = 'reading'">阅读</SecondaryButton>
        <SecondaryButton size="small" :aria-pressed="mode === 'raw'" @click="mode = 'raw'">原始文本</SecondaryButton>
      </div>
      <slot name="toolbar-end" />
    </div>
    <p v-if="document.offsetMs" class="mb-3 text-xs text-muted-color">全局偏移：{{ document.offsetMs }} 毫秒；时间列与原始文本标签一致。</p>
    <template v-if="mode === 'reading'">
      <div aria-label="可读歌词" role="table" class="text-sm leading-7">
        <div role="row" class="mb-2 grid grid-cols-[5.5rem_minmax(0,1fr)_minmax(0,1fr)] gap-3 border-b pb-2 text-xs text-muted-color">
          <span role="columnheader">时间</span><span role="columnheader">歌词</span><span role="columnheader">{{ extraChannel === 'translation' ? '翻译' : '罗马音' }}</span>
        </div>
        <div v-for="(row, index) in rows" :key="index" role="row" class="grid grid-cols-[5.5rem_minmax(0,1fr)_minmax(0,1fr)] gap-3 py-1">
          <span role="cell" class="select-none whitespace-nowrap text-xs tabular-nums text-muted-color">{{ lyricTime(row.timeMs) }}</span>
          <div role="cell" class="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere]">{{ row.text || '\u00a0' }}</div>
          <div role="cell" class="min-w-0 whitespace-pre-wrap text-muted-color [overflow-wrap:anywhere]">
            <span v-if="row.note" class="block text-xs">{{ row.note }}</span>
            <p v-for="(text, i) in row.extra" :key="i">{{ text || '\u00a0' }}</p>
          </div>
        </div>
      </div>
    </template>
    <template v-else>
      <p class="mb-3 text-xs text-muted-color">实际采用的规范化合并文本，并非未经处理的上游响应。</p>
      <pre aria-label="原始歌词文本" class="whitespace-pre-wrap font-sans text-sm leading-7 [overflow-wrap:anywhere]">{{ document.text }}</pre>
    </template>
  </section>
</template>
