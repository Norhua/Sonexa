<script setup lang="ts">
/**
 * 顶栏快速搜索与高级搜索面板。
 *
 * 一个表单，两个出口：本地结果就地筛选、上游结果跳转独立页面——
 * 两者的可用动作完全不同，不混在同一个列表里。
 */
import { ref } from 'vue'
import { Search, ChevronDown, X } from 'lucide-vue-next'
import { useLibraryStore } from '@/stores/library'
import { useSettingsStore } from '@/stores/settings'

const library = useLibraryStore()
const settings = useSettingsStore()

const emit = defineEmits<{
  toUpstream: [fields: { title: string; artist: string; album: string }]
}>()

const quick = ref('')
const advancedOpen = ref(false)

/** 高级搜索的条件。三个文本字段可任意留空。 */
const form = ref({
  title: '',
  artist: '',
  album: '',
  yearFrom: '',
  yearTo: '',
  tagId: 0,
  mediumFormat: '',
  country: '',
  primaryType: '',
  /** 作用域单选：当前歌单 / 全部曲目。 */
  scopeAll: false,
})

async function runQuick() {
  await library.quickSearch(quick.value.trim())
}

/** 搜索本地：不跳页，在当前曲目表格内筛选。 */
async function searchLocal() {
  const chips: { key: string; label: string }[] = []
  const next: Record<string, unknown> = {}

  if (form.value.title.trim()) {
    next.title = form.value.title.trim()
    chips.push({ key: 'title', label: `曲名="${form.value.title.trim()}"` })
  }
  if (form.value.artist.trim()) {
    next.artist = form.value.artist.trim()
    chips.push({ key: 'artist', label: `艺术家="${form.value.artist.trim()}"` })
  }
  if (form.value.album.trim()) {
    next.album = form.value.album.trim()
    chips.push({ key: 'album', label: `专辑="${form.value.album.trim()}"` })
  }
  const from = parseInt(form.value.yearFrom, 10)
  const to = parseInt(form.value.yearTo, 10)
  if (!Number.isNaN(from)) next.yearFrom = from
  if (!Number.isNaN(to)) next.yearTo = to
  if (!Number.isNaN(from) || !Number.isNaN(to)) {
    chips.push({ key: 'year', label: `年份=${form.value.yearFrom || '…'}–${form.value.yearTo || '…'}` })
  }
  if (form.value.tagId) {
    const tag = library.tags.find((t) => t.id === form.value.tagId)
    next.tagId = form.value.tagId
    chips.push({ key: 'tagId', label: `标签=#${tag?.name ?? form.value.tagId}` })
  }
  // 载体、国家、类型仅对本地搜索生效
  for (const key of ['mediumFormat', 'country', 'primaryType'] as const) {
    const value = form.value[key]
    if (value) {
      next[key] = value
      chips.push({ key, label: `${labelOf(key)}="${value}"` })
    }
  }

  // 作用域可切换：从「当前歌单」切到「全部曲目」。
  // 必须写进 store 的 scopeOverrideAll，否则 baseFilter() 会按当前视图
  // 重新推导出 collectionId，切换变成空操作。
  if (form.value.scopeAll) {
    library.scopeOverrideAll = true
  } else {
    library.scopeOverrideAll = false
  }

  await library.applyFilter(next, chips)
  advancedOpen.value = false
}

function labelOf(key: string) {
  return { mediumFormat: '载体', country: '国家', primaryType: '类型' }[key] ?? key
}

function reset() {
  form.value = {
    title: '', artist: '', album: '', yearFrom: '', yearTo: '',
    tagId: 0, mediumFormat: '', country: '', primaryType: '', scopeAll: false,
  }
}

/** 到上游搜索：携带已填的三个字段。 */
function toUpstream() {
  emit('toUpstream', {
    title: form.value.title.trim(),
    artist: form.value.artist.trim(),
    album: form.value.album.trim(),
  })
  advancedOpen.value = false
}
</script>

<template>
  <div class="relative flex-1">
    <div class="flex items-center gap-2">
      <div class="relative flex-1">
        <Search class="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
        <input
          v-model="quick"
          type="search"
          :placeholder="library.searchPlaceholder"
          class="w-full rounded-md border bg-background py-1.5 pl-8 pr-3 text-sm outline-none focus:ring-1 focus:ring-ring"
          @keyup.enter="runQuick"
        />
      </div>
      <button
        class="flex items-center gap-1 rounded-md border px-2 py-1.5 text-sm hover:bg-accent"
        @click="advancedOpen = !advancedOpen"
      >
        高级
        <ChevronDown :class="['size-3.5 transition-transform', advancedOpen ? 'rotate-180' : '']" />
      </button>
    </div>

    <!-- 高级搜索面板 -->
    <div
      v-if="advancedOpen"
      class="absolute left-0 right-0 top-full z-30 mt-1 space-y-3 rounded-md border bg-popover p-4 shadow-lg"
    >
      <div class="grid grid-cols-3 gap-3">
        <label class="text-xs">曲名
          <input v-model="form.title" class="mt-1 w-full rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">艺术家
          <input v-model="form.artist" class="mt-1 w-full rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">专辑
          <input v-model="form.album" class="mt-1 w-full rounded border bg-background px-2 py-1 text-sm" />
        </label>
      </div>

      <div class="flex flex-wrap items-end gap-3">
        <label class="text-xs">年份
          <div class="mt-1 flex items-center gap-1">
            <input v-model="form.yearFrom" class="w-16 rounded border bg-background px-2 py-1 text-sm" />
            <span class="text-muted-foreground">–</span>
            <input v-model="form.yearTo" class="w-16 rounded border bg-background px-2 py-1 text-sm" />
          </div>
        </label>
        <label class="text-xs">标签
          <select v-model="form.tagId" class="mt-1 rounded border bg-background px-2 py-1 text-sm">
            <option :value="0">全部</option>
            <option v-for="t in library.tags" :key="t.id" :value="t.id">#{{ t.name }}</option>
          </select>
        </label>
      </div>

      <div class="flex flex-wrap items-end gap-3">
        <label class="text-xs">载体
          <input v-model="form.mediumFormat" placeholder="全部"
            class="mt-1 w-24 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">国家
          <input v-model="form.country" placeholder="全部"
            class="mt-1 w-24 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <label class="text-xs">类型
          <input v-model="form.primaryType" placeholder="全部"
            class="mt-1 w-24 rounded border bg-background px-2 py-1 text-sm" />
        </label>
        <span class="pb-1 text-xs text-muted-foreground">（载体、国家、类型仅本地搜索有效）</span>
      </div>

      <div v-if="library.view === 'playlist' || library.view === 'tag'" class="flex items-center gap-4 text-xs">
        <span>作用域</span>
        <label class="flex items-center gap-1">
          <input v-model="form.scopeAll" type="radio" :value="false" /> 当前{{ library.view === 'tag' ? '标签' : '歌单' }}
        </label>
        <label class="flex items-center gap-1">
          <input v-model="form.scopeAll" type="radio" :value="true" /> 全部曲目
        </label>
      </div>

      <div class="flex justify-end gap-2 border-t pt-3">
        <button class="rounded-md border px-3 py-1.5 text-sm hover:bg-accent" @click="reset">重置</button>
        <button class="rounded-md border px-3 py-1.5 text-sm hover:bg-accent" @click="searchLocal">
          搜索本地
        </button>
        <button
          class="rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          :disabled="!settings.outboundAllowed"
          :title="!settings.outboundAllowed ? settings.outboundReason : ''"
          @click="toUpstream"
        >
          到上游搜索 →
        </button>
      </div>
    </div>

    <!-- 筛选 chip：标明生效的条件，可清除 -->
    <div v-if="library.chips.length > 0" class="absolute left-0 top-full mt-1 flex flex-wrap gap-1">
      <span class="text-xs text-muted-foreground">筛选:</span>
      <button
        v-for="chip in library.chips"
        :key="chip.key"
        class="flex items-center gap-1 rounded-full border bg-secondary px-2 py-0.5 text-xs hover:bg-accent"
        @click="library.removeChip(chip.key)"
      >
        {{ chip.label }}
        <X class="size-3" />
      </button>
    </div>
  </div>
</template>
