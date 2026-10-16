<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { Search } from 'lucide-vue-next'
import { App, duration, type Models } from '@/api'
import { useLyricsSearchStore, lyricFormat } from '@/stores/lyricsSearch'
import { useSettingsStore } from '@/stores/settings'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Dialog from '@/volt/Dialog.vue'
const store = useLyricsSearchStore()
const settings = useSettingsStore()
const sources = ref<Models.lyrics.Source[]>([])
let mounted = true
onBeforeUnmount(() => { mounted = false })
onMounted(async () => {
  try { const list = await App.ListLyricsSources(); if (mounted) sources.value = list }
  catch (e) { if (mounted) store.error = String(e) }
})
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
          <label class="text-xs">专辑（优先排序）<InputText v-model="store.album" class="mt-1 block" /></label>
          <Button type="submit" :disabled="!settings.outboundAllowed || !store.ready || store.busy"><Search class="size-4" />搜索</Button>
        </form>
        <div class="mt-3 flex flex-wrap items-center gap-4">
          <label v-for="source in sources" :key="source.id" class="flex items-center gap-2 text-sm"><Checkbox v-model="store.sources" :value="source.id" />{{ source.name }}</label>
        </div>
        <p v-if="!settings.outboundAllowed" class="mt-2 text-xs text-muted-color">出网未启用，请先在设置中配置联系方式。</p>
      </header>
      <p v-if="store.error" role="alert" class="px-4 py-2 text-sm text-danger-color">{{ store.error }}</p>
      <p v-if="store.notice" role="status" class="px-4 py-2 text-sm text-green-600">{{ store.notice }}</p>
      <div class="grid min-h-0 flex-1 grid-cols-[minmax(230px,30%)_minmax(0,1fr)]">
        <nav class="overflow-auto border-r p-3" aria-label="歌词候选">
          <p v-if="!Object.keys(store.results).length" class="p-3 text-sm text-muted-color">选择来源并搜索歌词</p>
          <section v-for="source in sources.filter(s => store.results[s.id])" :key="source.id" class="mb-4">
            <h3 class="mb-2 text-sm font-medium">{{ source.name }} ({{ store.results[source.id]?.cards.length ?? 0 }})</h3>
            <p v-if="store.results[source.id]?.state === 'loading'" role="status" class="text-xs text-muted-color">正在搜索…</p>
            <template v-else-if="store.results[source.id]?.state === 'error'">
              <p role="alert" class="break-words text-xs text-danger-color">{{ store.results[source.id]?.error }}</p>
              <SecondaryButton size="small" variant="text" :disabled="!settings.outboundAllowed || store.busy" @click="store.search(source.id)">重试 {{ source.name }}</SecondaryButton>
            </template>
            <p v-else-if="!store.results[source.id]?.cards.length" class="text-xs text-muted-color">无匹配歌曲</p>
            <button v-for="card in store.results[source.id]?.cards ?? []" :key="card.id" class="mb-2 block w-full rounded border p-3 text-left text-sm hover:bg-emphasis"
              :class="store.selected === card.id ? 'border-primary ring-1 ring-primary' : ''" :aria-pressed="store.selected === card.id" :disabled="store.busy" @click="store.select(card.id)">
              <span class="block break-words font-medium">{{ card.title || '未命名歌曲' }}</span>
              <span class="mt-1 block break-words text-xs text-muted-color">{{ card.artist }} · {{ card.album }}</span>
              <span class="mt-2 block text-xs">来源：{{ source.name }} · 时长：{{ duration(card.durationMs) }}</span>
              <span class="mt-1 block text-xs">类型：{{ store.loading[card.id] ? '获取中…' : store.failures[card.id] ? '获取失败 / 无歌词' : lyricFormat(store.documents[card.id]?.format) }}</span>
            </button>
          </section>
        </nav>
        <article class="min-w-0 overflow-auto p-6" aria-label="歌词预览">
          <template v-if="store.selection">
            <h3 class="mb-2 font-medium">{{ store.selection.title }}</h3>
            <p class="mb-5 text-xs text-muted-color">{{ store.selection.artist }} · {{ lyricFormat(store.document?.format) }}</p>
            <p v-if="store.loading[store.selection.id]" role="status" class="text-sm text-muted-color">正在获取歌词…</p>
            <template v-else-if="store.failures[store.selection.id]">
              <p role="alert" class="text-sm text-danger-color">{{ store.failures[store.selection.id] }}</p>
              <SecondaryButton class="mt-3" :disabled="!settings.outboundAllowed" @click="store.select(store.selection.id)">重新获取歌词</SecondaryButton>
            </template>
            <pre v-else-if="store.document" class="whitespace-pre-wrap break-words font-sans text-sm leading-7">{{ store.document.text }}</pre>
          </template>
          <p v-else class="py-10 text-center text-sm text-muted-color">选择左侧候选预览歌词；歌曲命中不代表已有歌词</p>
        </article>
      </div>
      <footer class="flex shrink-0 justify-end gap-3 border-t p-3">
        <SecondaryButton @click="store.close()">取消</SecondaryButton>
        <Button :disabled="!store.document || store.busy" @click="store.adopt()">{{ store.busy ? '应用中…' : '应用到歌曲' }}</Button>
      </footer>
    </section>
  </Dialog>
</template>
