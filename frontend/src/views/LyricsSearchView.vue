<script setup lang="ts">
import { computed, ref } from 'vue'
import { CircleAlert, LoaderCircle, RotateCw, Search, X } from 'lucide-vue-next'
import { duration } from '@/api'
import { useLyricsSearchStore } from '@/stores/lyricsSearch'
import { lyricSourceStatus, lyricFormat, lyricPhase, lyricFailure, lyricChannelStatus, lyricChannelBasis, lyricRoundFinished, type LyricExtraChannel } from '@/lib/lyricsPresentation'
import { useSettingsStore } from '@/stores/settings'
import LyricsPreview from '@/components/LyricsPreview.vue'
import LyricsScore from '@/components/LyricsScore.vue'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Dialog from '@/volt/Dialog.vue'
import Select from '@/volt/Select.vue'
const store = useLyricsSearchStore()
const settings = useSettingsStore()
const extraChannel = ref<LyricExtraChannel>('translation')
const extraOptions = [{ label: '翻译', value: 'translation' }, { label: '罗马音', value: 'romanization' }]
const searching = computed(() => Object.values(store.results).some(result => !lyricRoundFinished(result.phase)))
const searchFailed = computed(() => Object.values(store.results).some(result => result.state === 'error'))
const sourceName = (id: string) => store.sourceOptions.find(s => s.id === id)?.name ?? id
const sourceStatuses = computed(() => Object.fromEntries(Object.entries(store.results).map(([id, result]) => [id, lyricSourceStatus(result)])))
</script>

<template>
  <Dialog :visible="true" modal header="从网络搜索歌词" :style="{ width: '1200px', height: '820px' }"
    content-class="flex min-h-0 flex-1 p-0!" :close-button-props="{ 'aria-label': '关闭歌词搜索' }" @update:visible="store.close()">
    <section class="relative flex min-h-0 min-w-0 flex-1 flex-col" aria-label="从网络搜索歌词">
      <header class="shrink-0 border-b p-4">
        <form class="flex flex-wrap items-end gap-3" @submit.prevent="store.search()">
          <label class="text-xs">歌曲<InputText v-model="store.title" autofocus class="mt-1 block" /></label>
          <label class="text-xs">艺术家<InputText v-model="store.artist" class="mt-1 block" /></label>
          <label class="text-xs">时长<InputText v-model="store.durationInput" placeholder="分:秒" title="分:秒或秒数，未知请留空" class="mt-1 block w-28" /></label>
          <label class="text-xs">专辑（辅助匹配）<InputText v-model="store.album" class="mt-1 block" /></label>
          <Button type="submit" :disabled="!settings.outboundAllowed || !store.ready || store.busy"><Search class="size-4" />搜索</Button>
        </form>
        <div class="mt-3 flex flex-wrap items-center gap-4">
          <div v-for="source in store.sourceOptions" :key="source.id" :data-lyrics-source="source.id" class="flex items-center gap-1">
            <label class="flex items-center gap-2 text-sm"><Checkbox v-model="store.sources" :value="source.id" />{{ source.name }}</label>
            <template v-if="store.results[source.id]">
              <span role="status" :aria-label="`${source.name}：${sourceStatuses[source.id]!.label}`" :title="sourceStatuses[source.id]!.label" class="inline-flex size-5 shrink-0 items-center justify-center text-xs tabular-nums" :class="sourceStatuses[source.id]!.failed ? 'text-orange-600 dark:text-orange-400' : 'rounded-full border text-muted-color'">
                <CircleAlert v-if="sourceStatuses[source.id]!.failed" class="size-5" aria-hidden="true" />
                <template v-else>{{ sourceStatuses[source.id]!.count ?? '—' }}</template>
              </span>
              <button v-if="lyricRoundFinished(store.results[source.id]!.phase)" type="button" class="rounded p-1 text-muted-color hover:bg-emphasis focus-visible:outline focus-visible:outline-primary disabled:opacity-50" :aria-label="`重试 ${source.name}`" :title="`重试 ${source.name}`" :disabled="!settings.outboundAllowed || store.busy" @click="store.search(source.id)"><RotateCw class="size-3.5" aria-hidden="true" /></button>
              <span v-else role="status" :aria-label="`${source.name}：${lyricPhase(store.results[source.id]!.phase)}`"><LoaderCircle class="size-3.5 animate-spin text-muted-color" aria-hidden="true" /></span>
            </template>
          </div>
        </div>
        <p class="mt-2 text-xs text-muted-color">每来源按歌曲匹配预取前 3 项，再按歌词综合分跨来源排序。分数不是概率；等待所有来源结束后才自动预览合格高置信项，人工选择优先。</p>
        <p v-if="!settings.outboundAllowed" class="mt-2 text-xs text-muted-color">出网未启用，请先在设置中配置联系方式。</p>
      </header>
      <div v-if="store.error || store.notice" class="absolute right-4 bottom-20 z-10 flex max-w-[min(28rem,calc(100%-2rem))] items-start gap-3 rounded-lg border bg-surface-0 p-3 text-sm shadow-lg dark:bg-surface-900" :role="store.error ? 'alert' : 'status'">
        <span>{{ store.error || store.notice }}</span>
        <button type="button" aria-label="关闭歌词提示" class="shrink-0 rounded p-0.5 hover:bg-emphasis focus-visible:outline focus-visible:outline-primary" @click="store.error = ''; store.notice = ''"><X class="size-4" aria-hidden="true" /></button>
      </div>
      <div class="grid min-h-0 flex-1 grid-cols-[minmax(270px,36%)_minmax(0,1fr)]">
        <nav class="overflow-auto border-r p-3" aria-label="歌词候选">
          <p v-if="!Object.keys(store.results).length" class="p-3 text-sm text-muted-color">选择来源并搜索歌词</p>
          <p v-else-if="!store.cards.length" role="status" class="p-3 text-sm text-muted-color">{{ searching ? '正在搜索歌词…' : searchFailed ? '本次搜索未完成，请重试或更换来源。' : '无匹配歌曲' }}</p>
          <div v-for="card in store.cards" :key="card.id" :data-candidate-id="card.id" class="relative mb-3 rounded border p-3" :class="store.selected === card.id ? 'border-primary ring-1 ring-primary' : ''">
            <LyricsScore :candidate="card" />
            <button class="block w-full text-left text-sm hover:bg-emphasis" :aria-pressed="store.selected === card.id" :disabled="store.busy" @click="store.select(card.id)">
              <span class="block min-h-6 break-words pr-20 font-medium">{{ card.title || '未命名歌曲' }}</span>
              <span class="mt-1 block break-words text-xs text-muted-color">{{ card.artist }} · {{ card.album }}</span>
              <span class="mt-2 block text-xs">来源：{{ sourceName(card.source) }} · 时长：{{ duration(card.durationMs) }}</span>
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
            <p v-if="store.loading[store.selection.id]" role="status" class="text-sm text-muted-color">正在等待或获取歌词…</p>
            <template v-else-if="store.document">
              <LyricsPreview :document="store.document" :candidate-id="store.selection.id" :version="store.documentVersion" :extra-channel="extraChannel" @displayed="store.acknowledge">
                <template #toolbar-end>
                  <div class="ml-auto flex items-center gap-2 text-sm">
                    <label for="lyrics-extra-channel">额外展示：</label>
                    <Select v-model="extraChannel" input-id="lyrics-extra-channel" aria-label="额外展示" :options="extraOptions" option-label="label" option-value="value" size="small" class="w-28" />
                  </div>
                </template>
              </LyricsPreview>
              <SecondaryButton class="mt-4" size="small" :disabled="!settings.outboundAllowed || store.busy || !lyricRoundFinished(store.results[store.selection.source]?.phase ?? '')" @click="store.fetchSelected(true)">重新获取歌词</SecondaryButton>
            </template>
            <template v-else>
              <p v-if="store.failures[store.selection.id]" role="status" class="text-sm text-muted-color">{{ store.failures[store.selection.id] }}</p>
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
