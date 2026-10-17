<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { Search, Check } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import { useCoverSearchStore } from '@/stores/coverSearch'
import { useSettingsStore } from '@/stores/settings'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Select from '@/volt/Select.vue'
import Dialog from '@/volt/Dialog.vue'
import CoverPreview from '@/components/CoverPreview.vue'
const store = useCoverSearchStore()
const settings = useSettingsStore()
const sourceOptions = ref<Models.app.CoverSourceInfo[]>([])
const names = computed(() => Object.fromEntries(sourceOptions.value.map(source => [source.id, source.name])))
const filter = ref('all'), selected = ref<number | null>(null), busy = ref(false), notice = ref('')
const preview = ref<Models.app.CoverSearchCard | null>(null)
const variants = ref<Record<number, number>>({})
const dimensions = ref<Record<number, string>>({})
const countries = ref<Models.app.CountryOption[]>([])
const countryOptions = computed(() => [{ code: '', name: 'iTunes 默认地区' }, ...countries.value])
const cards = computed(() => store.cards.filter(card => filter.value === 'all' || card.source === filter.value))
const selection = computed(() => store.cards.flatMap(card => card.image.variants.map(v => ({ card, variant: v }))).find(item => item.variant.id === selected.value))
const emptyMessage = computed(() => {
  const results = Object.values(store.results).filter(result => filter.value === 'all' || result.source === filter.value)
  if (results.some(result => result.state === 'loading')) return '正在查询封面…'
  if (results.length && results.every(result => result.state === 'error')) return '搜索失败，请重试来源'
  if (results.some(result => result.state === 'error')) return '部分来源查询失败，请重试'
  return results.length ? '未找到图片' : '选择来源并搜索封面'
})
let mounted = true
onBeforeUnmount(() => { mounted = false })
onMounted(async () => {
  try {
    const [list, sources] = await Promise.all([App.ListCountries(), App.ListCoverSources()])
    if (!mounted) return
    countries.value = list
    sourceOptions.value = sources
  } catch (e) { if (mounted) store.error = String(e) }
})
function variant(card: Models.app.CoverSearchCard) {
  const available = card.image.variants
  // 卡片首次只下载同图最小档，不默认取 iTunes 3000 或网易云原图。
  const smallest = [...available].sort((a, b) => (Number(a.spec) || Infinity) - (Number(b.spec) || Infinity))[0]!
  return available.find(v => v.id === variants.value[card.image.id]) ?? smallest
}
function label(v: Models.domain.CoverVariant) {
  return dimensions.value[v.id] ?? (v.width && v.height ? `${v.width}×${v.height}` : v.spec === 'original' ? '原图·尺寸未知' : `${v.spec}px${v.constructed ? '·待验证' : ''}`)
}
function loaded(id: number, width: number, height: number) {
  dimensions.value[id] = `${width}×${height}`
}
function selectVariant(card: Models.app.CoverSearchCard, id: number) {
  variants.value[card.image.id] = id
  selected.value = id
}
async function search(only?: string) {
  if (!settings.outboundAllowed) return
  selected.value = null; preview.value = null; notice.value = ''
  await store.search(only)
}
async function adopt() {
  if (!selected.value) return
  busy.value = true
  try {
    const result = await store.adopt(selected.value)
    if (mounted && result) {
      dimensions.value[result.variantId] = `${result.width}×${result.height}`
      notice.value = `已应用到整个专辑：${result.width}×${result.height}（实际规格 ID ${result.variantId}）`
    }
  } catch (e) { if (mounted) store.error = String(e) }
  finally { busy.value = false }
}
</script>

<template>
  <Dialog :visible="true" modal header="从网络搜索封面" :style="{ width: '1200px', height: '820px' }"
    content-class="flex min-h-0 flex-1 p-0!" :close-button-props="{ 'aria-label': '关闭封面搜索' }"
    @update:visible="store.close()">
  <section class="flex min-h-0 min-w-0 flex-1 flex-col" aria-label="从网络搜索封面">
    <header class="border-b p-4">
      <p class="mb-3 text-sm text-muted-color">目标：{{ store.target?.track.album }} · 修改后应用到整个专辑</p>
      <form class="flex flex-wrap items-end gap-3" @submit.prevent="search()">
        <label class="text-xs">歌曲<InputText v-model="store.title" autofocus class="mt-1 block" /></label>
        <label class="text-xs">艺术家<InputText v-model="store.artist" class="mt-1 block" /></label>
        <label class="text-xs">专辑<InputText v-model="store.album" class="mt-1 block" /></label>
        <Button type="submit" :disabled="!settings.outboundAllowed || !store.ready"><Search class="size-4" />搜索</Button>
      </form>
      <div class="mt-3 flex flex-wrap items-center gap-4">
        <label v-for="source in sourceOptions" :key="source.id" class="flex items-center gap-2 text-sm">
          <Checkbox v-model="store.sources" :value="source.id" />{{ source.name }}
        </label>
        <Select v-if="store.sources.includes('itunes')" v-model="store.country" aria-label="iTunes 地区" :options="countryOptions" option-label="name" option-value="code" placeholder="iTunes 默认地区" size="small" />
      </div>
    </header>
    <nav class="flex flex-wrap gap-2 border-b px-4 py-2" aria-label="封面来源筛选">
      <SecondaryButton variant="text" :aria-pressed="filter === 'all'" @click="filter = 'all'">全部 ({{ store.cards.length }})</SecondaryButton>
      <div v-for="(name, id) in names" :key="id" class="flex items-center">
        <SecondaryButton variant="text" :aria-pressed="filter === id" @click="filter = id">{{ name }} ({{ store.results[id]?.state === 'loading' ? '加载中' : store.results[id]?.cards?.length ?? 0 }})</SecondaryButton>
        <SecondaryButton v-if="store.results[id]?.state === 'error'" size="small" variant="text" :disabled="!settings.outboundAllowed" @click="search(id)">失败·重试</SecondaryButton>
      </div>
    </nav>
    <p v-if="store.error" role="alert" class="px-4 py-2 text-sm text-danger-color">{{ store.error }}</p>
    <p v-if="notice" role="status" class="px-4 py-2 text-sm text-primary">{{ notice }}</p>
    <div class="min-h-0 flex-1 overflow-auto p-4">
      <p v-for="result in Object.values(store.results).filter(r => r.state === 'error')" :key="result.source" class="mb-2 text-sm text-danger-color">{{ names[result.source] }}：{{ result.error }}</p>
      <p v-if="!cards.length" role="status" class="py-10 text-center text-muted-color">{{ emptyMessage }}</p>
      <div class="grid grid-cols-[repeat(auto-fill,minmax(190px,1fr))] gap-4">
        <article v-for="card in cards" :key="`${card.source}-${card.image.id}`" class="overflow-hidden rounded-lg border" :class="selected === variant(card).id ? 'ring-2 ring-primary' : ''">
          <button class="relative block aspect-square w-full bg-surface-100 dark:bg-surface-800" :aria-label="`选择 ${names[card.source]} ${card.image.title}`" @click="selected = variant(card).id" @dblclick="preview = card">
            <CoverPreview :variant="variant(card)" :search-id="store.results[card.source]?.searchId" @verified="loaded" />
            <div class="absolute bottom-2 left-2 flex flex-wrap gap-1 text-xs font-medium text-white">
              <span class="rounded bg-pink-600 px-2 py-1">{{ names[card.source] }}</span><span class="rounded bg-pink-600 px-2 py-1">{{ label(variant(card)) }}</span>
            </div>
            <Check v-if="selected === variant(card).id" class="absolute right-2 top-2 size-6 rounded-full bg-primary p-1 text-white" />
          </button>
          <div class="space-y-2 p-3">
            <p class="truncate text-sm font-medium">{{ card.image.title || store.album }}</p>
            <p class="truncate text-xs text-muted-color">{{ card.image.artist }} · {{ card.image.primaryType }}</p>
            <div class="flex flex-wrap gap-1">
              <SecondaryButton v-for="v in card.image.variants" :key="v.id" size="small" variant="outlined" @click="selectVariant(card, v.id)">{{ label(v) }}</SecondaryButton>
            </div>
            <SecondaryButton size="small" variant="text" @click="preview = card">查看大图</SecondaryButton>
          </div>
        </article>
      </div>
    </div>
    <footer class="flex shrink-0 items-center justify-end gap-3 border-t p-3">
      <span v-if="selection" class="mr-auto text-sm">已选：{{ names[selection.card.source] }} / {{ selection.card.image.primaryType }} / {{ label(selection.variant) }}</span>
      <SecondaryButton @click="store.close()">取消</SecondaryButton><Button :disabled="!selection || busy" @click="adopt">{{ busy ? '应用中…' : '应用到专辑' }}</Button>
    </footer>
    <Dialog :visible="!!preview" modal header="封面预览" class="w-[min(85vw,900px)]" @update:visible="preview = null">
      <CoverPreview v-if="preview" :variant="variant(preview)" :search-id="store.results[preview.source]?.searchId" large @verified="loaded" />
    </Dialog>
  </section>
  </Dialog>
</template>
