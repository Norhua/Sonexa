<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { lyricTime, type LyricDocument } from '@/lib/lyricsPresentation'
import SecondaryButton from '@/volt/SecondaryButton.vue'
const props = defineProps<{ document: LyricDocument; candidateId: string; version: number }>()
const emit = defineEmits<{ displayed: [id: string, version: number] }>()
const mode = ref<'reading' | 'raw'>('reading')
const channelName = (channel: string) => ({ translation: '翻译', romanization: '罗马音', additional: '附加文本（归属未确认）' }[channel] ?? '')
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
    <div class="mb-4 flex flex-wrap gap-2" role="group" aria-label="预览模式">
      <SecondaryButton size="small" :aria-pressed="mode === 'reading'" @click="mode = 'reading'">阅读</SecondaryButton>
      <SecondaryButton size="small" :aria-pressed="mode === 'raw'" @click="mode = 'raw'">原始文本</SecondaryButton>
    </div>
    <p v-if="document.offsetMs" class="mb-3 text-xs text-muted-color">全局偏移：{{ document.offsetMs }} 毫秒；时间列与原始文本标签一致。</p>
    <template v-if="mode === 'reading'">
      <div aria-label="可读歌词" class="space-y-1 text-sm leading-7">
        <div v-for="(row, index) in document.rows" :key="index" class="grid grid-cols-[6.5rem_minmax(0,1fr)] gap-3">
          <span class="select-none text-xs tabular-nums text-muted-color">{{ lyricTime(row.timeMs) }}</span>
          <div class="min-w-0 whitespace-pre-wrap [overflow-wrap:anywhere]">
            <span v-if="row.channel !== 'original'" class="mr-2 text-xs text-muted-color">{{ channelName(row.channel) }}{{ row.timeMs == null ? '（未同步）' : '' }}</span><span>{{ row.text || '\u00a0' }}</span>
            <p v-for="(text, i) in row.translation" :key="`t${i}`" class="text-muted-color"><span class="mr-2 text-xs">翻译</span>{{ text }}</p>
            <p v-for="(text, i) in row.romanization" :key="`r${i}`" class="text-muted-color"><span class="mr-2 text-xs">罗马音</span>{{ text }}</p>
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
