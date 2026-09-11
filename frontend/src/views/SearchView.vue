<script setup lang="ts">
/**
 * 搜索页。
 *
 * 一个页面两种范围：资料库或某一个上游，右上角下拉单选。
 * 做成下拉而不是平铺开关，是因为上游数量会增长——平铺的选项条会越来越长，
 * 而一次搜索只可能用其中一个。
 *
 * 范围单选还解决了一个实际问题：多上游并发时结果落进同一个列表，
 * 来源难分、排序也不可比（各家的 relevance 不同源）。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { ChevronDown, Search as SearchIcon } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import AdvancedSearchPanel from '@/components/AdvancedSearchPanel.vue'
import CandidateTable from '@/components/CandidateTable.vue'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import { useSettingsStore } from '@/stores/settings'
import { LibraryScope, useSearchStore } from '@/stores/search'

const settings = useSettingsStore()
const search = useSearchStore()

const emit = defineEmits<{
  openTrack: [row: Row]
  editTrack: [row: Row]
  addToCollection: [rows: Row[], kind: 'playlist' | 'tag']
  viewArtist: [row: Row]
  copyInfo: [rows: Row[]]
  removeTracks: [rows: Row[]]
  selectionChange: [ids: number[]]
  exportScope: [scope: 'playlist' | 'filtered' | 'selection' | 'all']
  jumpLocal: [trackId: number]
  goSettings: []
}>()

const scopeOpen = ref(false)
const scopeRoot = ref<HTMLElement | null>(null)
const countries = ref<Models.main.CountryOption[]>([])
const hint = ref<Models.main.CountryHint | null>(null)
const countryTouched = ref(false)

/** 范围下拉的选项：资料库固定在最前，上游从注册表动态渲染。 */
const scopeOptions = computed(() => [
  { id: LibraryScope, label: '资料库', configured: true, reason: '' },
  ...settings.providers.map((p) => ({
    id: p.id,
    label: p.displayName,
    configured: p.configured,
    reason: p.reason,
  })),
])

const scopeLabel = computed(
  () => scopeOptions.value.find((o) => o.id === search.scope)?.label ?? '资料库',
)

/** 地区下拉仅在当前上游需要地区参数时出现。 */
const showCountry = computed(() => !search.isLibrary && settings.anyNeedsCountry([search.scope]))

const providerCaps = computed(() => {
  const map: Record<string, string[]> = {}
  for (const p of settings.providers) map[p.id] = p.capabilities ?? []
  return map
})

const rows = computed<Row[]>(() =>
  search.libraryRows.map((t) => ({
    id: t.id,
    title: t.title,
    artist: t.artist,
    album: t.album,
    year: t.year,
    trackNumber: t.trackNumber,
    discNumber: t.discNumber,
    durationMs: t.durationMs,
    genre: t.genre,
    mediumFormat: t.mediumFormat,
    country: t.country,
    coverKey: t.coverKey,
  })),
)

const canSearch = computed(() => {
  if (search.loading) return false
  if (search.isLibrary) return true
  return settings.outboundAllowed
})

onMounted(async () => {
  countries.value = (await App.ListCountries()) ?? []
  window.addEventListener('pointerdown', onPointerDown)
})
onUnmounted(() => window.removeEventListener('pointerdown', onPointerDown))

function onPointerDown(event: PointerEvent) {
  if (scopeOpen.value && scopeRoot.value && !scopeRoot.value.contains(event.target as Node)) {
    scopeOpen.value = false
  }
}

function pickScope(id: string) {
  search.setScope(id)
  scopeOpen.value = false
}

async function updateHint() {
  const text = `${search.query} ${search.valueOf('artist')} ${search.valueOf('album')}`.trim()
  if (!text) return
  hint.value = await App.SuggestCountry(text)
  // 探测结果仅作建议：用户已手动选择过就不覆盖
  if (hint.value?.detected && !countryTouched.value) search.country = hint.value.country
}

async function run() {
  if (!search.isLibrary && showCountry.value) await updateHint()
  await search.run()
}

async function addToLibrary(row: Models.main.CandidateRow) {
  try {
    await App.AddToLibrary(row)
    row.action = '已在库中'
    if (!row.badges.includes('本地')) row.badges = ['本地', ...row.badges]
  } catch (e) {
    search.error = String(e)
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <!-- 页头：搜索框居中，范围下拉在右上角 -->
    <header class="shrink-0 border-b px-4 py-3">
      <div class="flex items-center gap-3">
        <div class="relative mx-auto w-full max-w-xl">
          <SearchIcon
            class="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground"
          />
          <input
            v-model="search.query"
            type="search"
            :placeholder="search.isLibrary ? '搜索资料库' : `在 ${scopeLabel} 中搜索`"
            class="w-full rounded-md border bg-background py-1.5 pl-9 pr-3 text-sm outline-none focus:ring-1 focus:ring-ring"
            @keyup.enter="run"
          />
        </div>

        <AdvancedSearchPanel @run="run" />

        <!-- 范围：单选下拉，上游从注册表渲染 -->
        <div ref="scopeRoot" class="relative shrink-0">
          <button
            class="flex items-center gap-1 rounded-md border px-2.5 py-1.5 text-sm hover:bg-accent"
            @click="scopeOpen = !scopeOpen"
          >
            {{ scopeLabel }}
            <ChevronDown :class="['size-3.5 transition-transform', scopeOpen ? 'rotate-180' : '']" />
          </button>
          <div
            v-if="scopeOpen"
            class="absolute right-0 top-full z-30 mt-1 min-w-40 rounded-md border bg-popover py-1 text-sm shadow-md"
          >
            <button
              v-for="o in scopeOptions"
              :key="o.id"
              :class="[
                'block w-full px-3 py-1.5 text-left hover:bg-accent',
                search.scope === o.id ? 'font-medium' : '',
              ]"
              @click="pickScope(o.id)"
            >
              {{ o.label }}
              <span v-if="!o.configured" class="text-xs text-muted-foreground" :title="o.reason">
                （未配置）
              </span>
            </button>
          </div>
        </div>

        <button
          class="shrink-0 rounded-md bg-primary px-3 py-1.5 text-sm text-primary-foreground hover:bg-primary/90 disabled:opacity-50"
          :disabled="!canSearch"
          :title="!search.isLibrary && !settings.outboundAllowed ? settings.outboundReason : ''"
          @click="run"
        >
          {{ search.loading ? '搜索中…' : '搜索' }}
        </button>
      </div>

      <!-- 地区下拉仅在当前上游需要地区参数时出现 -->
      <div v-if="showCountry" class="mt-2 flex items-center justify-end gap-2 text-xs">
        <label class="flex items-center gap-1">
          地区
          <select
            v-model="search.country"
            class="rounded border bg-background px-1.5 py-0.5"
            @change="countryTouched = true"
          >
            <option value="">跟随默认</option>
            <option v-for="c in countries" :key="c.code" :value="c.code">{{ c.name }}</option>
          </select>
        </label>
        <span v-if="hint?.detected" class="text-muted-foreground">{{ hint.message }}</span>
      </div>
    </header>

    <!-- 出网禁用时把原因说清楚，而不是让搜索按钮无声地灰着 -->
    <p
      v-if="!search.isLibrary && !settings.outboundAllowed"
      class="flex shrink-0 items-center gap-2 border-b bg-secondary px-4 py-2 text-xs"
    >
      <span class="flex-1">{{ settings.outboundReason }}</span>
      <button class="rounded border px-2 py-0.5 hover:bg-accent" @click="emit('goSettings')">
        前往设置
      </button>
    </p>

    <div class="min-h-0 flex-1 overflow-auto">
      <!-- 资料库结果：与其他视图共用同一个曲目表格 -->
      <TrackTable
        v-if="search.isLibrary && search.searched"
        :rows="rows"
        :loading="search.loading"
        @open="(r) => emit('openTrack', r)"
        @edit="(r) => emit('editTrack', r)"
        @add-to-collection="(r, k) => emit('addToCollection', r, k)"
        @view-artist="(r) => emit('viewArtist', r)"
        @copy-info="(r) => emit('copyInfo', r)"
        @remove="(r) => emit('removeTracks', r)"
        @selection-change="(ids) => emit('selectionChange', ids)"
        @export-scope="(s) => emit('exportScope', s)"
      >
        <template #empty>资料库里没有匹配的曲目</template>
      </TrackTable>

      <!-- 上游结果：与复核队列共用同一个候选表格 -->
      <CandidateTable
        v-else-if="!search.isLibrary && search.searched"
        :rows="search.upstreamRows"
        :capabilities="search.upstreamCaps"
        :provider-capabilities="providerCaps"
        @act="addToLibrary"
        @jump-local="(id) => emit('jumpLocal', id)"
      >
        <template #empty>没有搜到结果</template>
      </CandidateTable>

      <div v-else class="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <SearchIcon class="size-12 opacity-40" />
        <p class="text-base font-semibold">
          {{ search.isLibrary ? '搜索你的资料库' : `在 ${scopeLabel} 中搜索` }}
        </p>
        <p class="text-xs">条件可任意留空；需要更精确时用「高级」添加条件</p>
      </div>
    </div>

    <p v-if="search.error" class="shrink-0 border-t bg-destructive/10 px-4 py-2 text-xs text-destructive">
      {{ search.error }}
    </p>
  </div>
</template>
