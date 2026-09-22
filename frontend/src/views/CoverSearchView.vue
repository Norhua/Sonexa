<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowLeft, Search, Check } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import { useCoverSearchStore } from '@/stores/coverSearch'
import { useSettingsStore } from '@/stores/settings'
import Button from '@/volt/Button.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import InputText from '@/volt/InputText.vue'
import Checkbox from '@/volt/Checkbox.vue'
import Select from '@/volt/Select.vue'
import Dialog from '@/volt/Dialog.vue'
const store = useCoverSearchStore()
const settings = useSettingsStore()
const names: Record<string, string> = { caa: 'CAA', itunes: 'iTunes', qq: 'QQ', '163': '网易云' }
const filter = ref('all'), selected = ref<number | null>(null), busy = ref(false), notice = ref('')
const preview = ref<Models.app.CoverSearchCard | null>(null)
const variants = ref<Record<number, number>>({})
const dimensions = ref<Record<number, string>>({})
const countries = ref<Models.app.CountryOption[]>([])
const cards = computed(() => store.cards.filter(card => filter.value === 'all' || card.source === filter.value))
onMounted(async () => { countries.value = await App.ListCountries() })
function variant(card: Models.app.CoverSearchCard) {
  return card.image.variants.find(v => v.id === variants.value[card.image.id]) ?? card.image.variants[0]!
}
function label(v: Models.store.CoverVariant) {
  return dimensions.value[v.id] ?? (v.width && v.height ? `${v.width}×${v.height}` : v.spec === 'original' ? '原图·尺寸未知' : `${v.spec}px${v.constructed ? '·待验证' : ''}`)
}
function loaded(v: Models.store.CoverVariant, event: Event) {
  const image = event.target as HTMLImageElement
  dimensions.value[v.id] = `${image.naturalWidth}×${image.naturalHeight}`
}
function selectVariant(card: Models.app.CoverSearchCard, id: number) {
  variants.value[card.image.id] = id
  selected.value = id
}
async function search(only?: string) { selected.value = null; notice.value = ''; await store.search(only) }
async function adopt() {
  if (!selected.value) return
  busy.value = true
  try {
    const result = await store.adopt(selected.value)
    notice.value = `已应用到整个专辑：${result.width}×${result.height}`
  } catch (e) { store.error = String(e) }
  finally { busy.value = false }
}
</script>

<template>
  <section class="flex min-h-0 flex-1 flex-col" aria-label="从网络搜索封面">
    <header class="border-b p-4">
      <div class="mb-3 flex items-center gap-3">
        <SecondaryButton variant="text" @click="store.close()"><ArrowLeft class="size-4" />返回</SecondaryButton>
        <h1 class="text-lg font-semibold">从网络搜索封面</h1>
        <span class="text-sm text-muted-color">{{ store.target?.track.album }} · 修改后应用到整个专辑</span>
      </div>
      <form class="flex flex-wrap items-end gap-3" @submit.prevent="search()">
        <label class="text-xs">歌曲<InputText v-model="store.title" class="mt-1 block" /></label>
        <label class="text-xs">艺术家<InputText v-model="store.artist" class="mt-1 block" /></label>
        <label class="text-xs">专辑<InputText v-model="store.album" class="mt-1 block" /></label>
        <Button type="submit" :disabled="!settings.outboundAllowed"><Search class="size-4" />搜索</Button>
      </form>
      <div class="mt-3 flex flex-wrap items-center gap-4">
        <label v-for="(name, id) in names" :key="id" class="flex items-center gap-2 text-sm">
          <Checkbox v-model="store.sources" :value="id" />{{ name }}
        </label>
        <Select v-if="store.sources.includes('itunes')" v-model="store.country" :options="countries" option-label="name" option-value="code" placeholder="iTunes 默认地区" size="small" />
      </div>
    </header>
    <nav class="flex flex-wrap gap-2 border-b px-4 py-2" aria-label="封面来源筛选">
      <SecondaryButton variant="text" @click="filter = 'all'">全部 ({{ store.cards.length }})</SecondaryButton>
      <div v-for="(name, id) in names" :key="id" class="flex items-center">
        <SecondaryButton variant="text" @click="filter = id">{{ name }} ({{ store.results[id]?.state === 'loading' ? '加载中' : store.results[id]?.cards?.length ?? 0 }})</SecondaryButton>
        <SecondaryButton v-if="store.results[id]?.state === 'error'" size="small" variant="text" @click="search(id)">失败·重试</SecondaryButton>
      </div>
    </nav>
    <p v-if="store.error" role="alert" class="px-4 py-2 text-sm text-danger-color">{{ store.error }}</p>
    <p v-if="notice" role="status" class="px-4 py-2 text-sm text-primary">{{ notice }}</p>
    <div class="min-h-0 flex-1 overflow-auto p-4">
      <p v-for="result in Object.values(store.results).filter(r => r.state === 'error')" :key="result.source" class="mb-2 text-sm text-danger-color">{{ names[result.source] }}：{{ result.error }}</p>
      <p v-if="!cards.length" class="py-10 text-center text-muted-color">{{ Object.values(store.results).some(r => r.state === 'loading') ? '正在查询封面…' : Object.values(store.results).some(r => r.state === 'error') ? '部分来源查询失败，请重试' : Object.keys(store.results).length ? '未找到图片' : '选择来源并搜索封面' }}</p>
      <div class="grid grid-cols-[repeat(auto-fill,minmax(190px,1fr))] gap-4">
        <article v-for="card in cards" :key="`${card.source}-${card.image.id}`" class="overflow-hidden rounded-lg border" :class="selected === variant(card).id ? 'ring-2 ring-primary' : ''">
          <button class="relative block aspect-square w-full bg-surface-100 dark:bg-surface-800" :aria-label="`选择 ${names[card.source]} ${card.image.title}`" @click="selected = variant(card).id" @dblclick="preview = card">
            <img :src="`/cover?variantId=${variant(card).id}`" alt="封面预览" loading="lazy" class="size-full object-contain" @load="loaded(variant(card), $event)" />
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
    <footer class="flex shrink-0 justify-end gap-3 border-t p-3">
      <SecondaryButton @click="store.close()">取消</SecondaryButton><Button :disabled="!selected || busy" @click="adopt">{{ busy ? '应用中…' : '应用到专辑' }}</Button>
    </footer>
    <Dialog :visible="!!preview" modal header="封面预览" class="w-[min(85vw,900px)]" @update:visible="preview = null">
      <img v-if="preview" :src="`/cover?variantId=${variant(preview).id}`" alt="封面大图" class="max-h-[70vh] w-full object-contain" />
    </Dialog>
  </section>
</template>
