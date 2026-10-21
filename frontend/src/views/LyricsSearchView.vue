<script setup lang="ts">
import { Search } from 'lucide-vue-next'
import { duration } from '@/api'
import { useLyricsSearchStore } from '@/stores/lyricsSearch'
import { lyricFormat, lyricPhase, lyricFailure, lyricChannelStatus, lyricChannelBasis, lyricRoundFinished } from '@/lib/lyricsPresentation'
import { useSettingsStore } from '@/stores/settings'
import LyricsPreview from '@/components/LyricsPreview.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Dialog from '@/volt/Dialog.vue'
const store = useLyricsSearchStore()
const settings = useSettingsStore()
const sourceName = (id: string) => store.sourceOptions.find(s => s.id === id)?.name ?? id
const timingStatus = (status?: string) => ({ available: '已获取', unavailable: '上游未提供', error: '失败', unsupported: '此来源暂未接入' }[status ?? ''] ?? '待检查')
</script>

<template>
  <Dialog :visible="true" modal header="从网络搜索歌词" :style="{ width: '1200px', height: '820px' }"
    content-class="flex min-h-0 flex-1 p-0!" :close-button-props="{ 'aria-label': '关闭歌词搜索' }" @update:visible="store.close()">
    <section class="flex min-h-0 min-w-0 flex-1 flex-col" aria-label="从网络搜索歌词">
      <header class="shrink-0 border-b p-4">
        <p class="mb-3 text-sm text-muted-color">目标：{{ store.target?.track.title || '未命名歌曲' }} · 仅修改当前歌曲的歌词</p>
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="store.search()">
          <label class="text-xs">歌曲<InputText v-model="store.title" autofocus class="mt-1 block" /></label>
          <label class="text-xs">艺术家<InputText v-model="store.artist" class="mt-1 block" /></label>
          <label class="text-xs">专辑（辅助匹配）<InputText v-model="store.album" class="mt-1 block" /></label>
          <Button type="submit" :disabled="!settings.outboundAllowed || !store.ready || store.busy"><Search class="size-4" />搜索</Button>
        </form>
        <div class="mt-3 flex flex-wrap items-center gap-4">
          <label v-for="source in store.sourceOptions" :key="source.id" class="flex items-center gap-2 text-sm"><Checkbox v-model="store.sources" :value="source.id" />{{ source.name }}</label>
        </div>
        <p class="mt-2 text-xs text-muted-color">每来源按歌曲匹配预取前 3 项，再按歌词综合分跨来源排序。分数不是概率；等待所有来源结束后才自动预览合格高置信项，人工选择优先。</p>
        <p v-if="!settings.outboundAllowed" class="mt-2 text-xs text-muted-color">出网未启用，请先在设置中配置联系方式。</p>
        <div class="mt-2 flex max-h-24 flex-wrap gap-x-4 gap-y-1 overflow-auto text-xs" aria-label="歌词来源进度">
          <div v-for="(result, source) in store.results" :key="source">
            <span role="status">{{ sourceName(String(source)) }}：{{ lyricPhase(result.phase) }} · {{ result.cards.length }} 首
              <template v-if="result.progress"> · 预取 {{ result.progress.selected - result.progress.incomplete }}/{{ result.progress.selected }}（成功 {{ result.progress.success }}、无歌词 {{ result.progress.noLyrics }}、失败 {{ result.progress.failed }}、未完成 {{ result.progress.incomplete }}）</template>
            </span>
            <span v-if="result.state === 'ok' && !result.cards.length && lyricRoundFinished(result.phase)"> · 无匹配歌曲</span>
            <span v-if="result.error" role="alert" class="text-danger-color"> {{ result.error }}</span>
            <SecondaryButton v-if="lyricRoundFinished(result.phase)" size="small" variant="text" :disabled="!settings.outboundAllowed || store.busy" @click="store.search(String(source))">重试 {{ sourceName(String(source)) }}</SecondaryButton>
          </div>
        </div>
      </header>
      <p v-if="store.error" role="alert" class="px-4 py-2 text-sm text-danger-color">{{ store.error }}</p>
      <p v-if="store.notice" role="status" class="px-4 py-2 text-sm text-green-600">{{ store.notice }}</p>
      <div class="grid min-h-0 flex-1 grid-cols-[minmax(270px,36%)_minmax(0,1fr)]">
        <nav class="overflow-auto border-r p-3" aria-label="歌词候选">
          <p v-if="!Object.keys(store.results).length" class="p-3 text-sm text-muted-color">选择来源并搜索歌词</p>
          <div v-for="card in store.cards" :key="card.id" :data-candidate-id="card.id" class="mb-3 rounded border p-3" :class="store.selected === card.id ? 'border-primary ring-1 ring-primary' : ''">
            <button class="block w-full text-left text-sm hover:bg-emphasis" :aria-pressed="store.selected === card.id" :disabled="store.busy" @click="store.select(card.id)">
              <span class="block break-words font-medium">{{ card.title || '未命名歌曲' }}</span>
              <span class="mt-1 block break-words text-xs text-muted-color">{{ card.artist }} · {{ card.album }}</span>
              <span class="mt-2 block text-xs">来源：{{ sourceName(card.source) }} · 时长：{{ duration(card.durationMs) }}</span>
              <span class="mt-1 block text-xs">匹配：{{ card.highConfidence ? '高置信' : '需确认' }} · 歌曲匹配 {{ card.matchScore }} 分 · 歌词综合 {{ card.totalScore ?? '—' }}</span>
              <span class="mt-1 block text-xs text-muted-color">{{ card.matchNotes?.join('；') }}</span>
              <span class="mt-1 block text-xs">类型：{{ lyricFormat(card.format) }} · {{ card.fetchState === 'ready' ? '已获取' : card.fetchState === 'queued' ? '排队中' : card.fetchState === 'loading' ? '获取中' : card.fetchState === 'pending' ? (card.prefetch ? '待预取' : '未预取，手动获取') : lyricFailure(card.stopReason || card.errorKind) }}</span>
              <span class="mt-2 flex flex-wrap gap-2 text-xs" aria-label="候选通道状态">
                <span class="rounded bg-emphasis px-2 py-1" :title="lyricChannelBasis(card.translation)">翻译：{{ lyricChannelStatus(card.translation) }}</span>
                <span class="rounded bg-emphasis px-2 py-1" :title="lyricChannelBasis(card.romanization)">罗马音：{{ lyricChannelStatus(card.romanization) }}</span>
              </span>
            </button>
            <details class="mt-2 text-xs">
              <summary class="cursor-pointer text-muted-color">评分明细（非概率）</summary>
              <p v-for="term in card.matchTerms" :key="`m${term.code}`">歌曲 · {{ term.label }}：{{ term.value }}</p>
              <p v-for="term in card.scoreTerms" :key="`s${term.code}`">综合 · {{ term.label }}：{{ term.value }}</p>
              <p v-if="card.totalScore == null">未取得有效原文，尚无综合分。</p>
            </details>
          </div>
        </nav>
        <article class="min-w-0 overflow-auto p-6" aria-label="歌词预览">
          <template v-if="store.selection">
            <h3 class="mb-2 font-medium">{{ store.selection.title }}</h3>
            <p class="mb-5 text-xs text-muted-color">{{ store.selection.artist }} · {{ lyricFormat(store.document?.format) }}</p>
            <p v-if="store.loading[store.selection.id]" role="status" class="text-sm text-muted-color">正在等待或获取歌词…</p>
            <template v-else-if="store.document">
              <div class="mb-4 space-y-1 text-xs text-muted-color" aria-label="歌词通道状态">
                <p>翻译：{{ lyricChannelStatus(store.selection.translation) }} · 罗马音：{{ lyricChannelStatus(store.selection.romanization) }}</p>
                <p>{{ lyricChannelBasis(store.selection.translation) }}</p>
                <p>{{ lyricChannelBasis(store.selection.romanization) }}</p>
                <p>逐字时间：{{ timingStatus(store.document.wordTiming) }}</p>
                <p v-for="warning in store.document.warnings" :key="warning" role="status">{{ warning }}</p>
              </div>
              <LyricsPreview :document="store.document" :candidate-id="store.selection.id" :version="store.documentVersion" @displayed="store.acknowledge" />
              <SecondaryButton class="mt-4" size="small" :disabled="!settings.outboundAllowed || store.busy || !lyricRoundFinished(store.results[store.selection.source]?.phase ?? '')" @click="store.fetchSelected(true)">重新获取歌词</SecondaryButton>
            </template>
            <template v-else>
              <p v-if="store.failures[store.selection.id]" role="alert" class="text-sm text-danger-color">{{ store.failures[store.selection.id] }}</p>
              <p v-else class="text-sm text-muted-color">{{ store.selection.fetchState === 'pending' ? '未预取此候选，请手动获取歌词。' : lyricFailure(store.selection.stopReason || store.selection.errorKind) }}</p>
              <p v-if="!lyricRoundFinished(store.results[store.selection.source]?.phase ?? '')" class="mt-2 text-xs text-muted-color">请等待本来源预取结束。</p>
              <SecondaryButton class="mt-3" :disabled="!settings.outboundAllowed || store.busy || !lyricRoundFinished(store.results[store.selection.source]?.phase ?? '')" @click="store.fetchSelected()">手动获取歌词</SecondaryButton>
            </template>
          </template>
          <p v-else class="py-10 text-center text-sm text-muted-color">选择左侧候选预览歌词；歌曲命中不代表已有歌词</p>
        </article>
      </div>
      <footer class="flex shrink-0 justify-end gap-3 border-t p-3">
        <SecondaryButton @click="store.close()">取消</SecondaryButton>
        <Button :disabled="!store.canAdopt" @click="store.adopt()">{{ store.busy ? '应用中…' : '应用到歌曲' }}</Button>
      </footer>
    </section>
  </Dialog>
</template>
