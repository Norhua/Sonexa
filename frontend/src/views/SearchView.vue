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
import { computed, onMounted, ref } from 'vue'
import { Search as SearchIcon } from 'lucide-vue-next'
import { App, type Models } from '@/api'
import AdvancedSearchPanel from '@/components/AdvancedSearchPanel.vue'
import PaneHeader from '@/components/PaneHeader.vue'
import CandidateTable from '@/components/CandidateTable.vue'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import Button from '@/volt/Button.vue'
import InputText from '@/volt/InputText.vue'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import Select from '@/volt/Select.vue'
import { useSettingsStore } from '@/stores/settings'
import { LibraryScope, useSearchStore } from '@/stores/search'

defineProps<{ drawerOpen?: boolean }>()
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

const countries = ref<Models.app.CountryOption[]>([])
const hint = ref<Models.app.CountryHint | null>(null)
const countryTouched = ref(false)

/**
 * 范围下拉的选项：资料库固定在最前，上游从注册表动态渲染。
 *
 * 未配置的上游仍然列出但在标签后注明——把它藏起来，用户会以为这个上游不存在。
 */
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
    addedAt: t.addedAt,
    trackNumber: t.trackNumber,
    discNumber: t.discNumber,
    durationMs: t.durationMs,
    genre: t.genre,
    mediumFormat: t.mediumFormat,
    country: t.country,
    coverSource: { provider: t.matchSource, ref: t.releaseRef, rg: t.releaseGroupRef },
    adoptedCoverPath: t.coverPath,
  })),
)

const canSearch = computed(() => {
  if (search.loading) return false
  if (search.isLibrary) return true
  return settings.outboundAllowed
})

onMounted(async () => {
  countries.value = (await App.ListCountries()) ?? []
})

/** 地区下拉的选项：「跟随默认」置顶。 */
const countryOptions = computed(() => [
  { code: '', name: '跟随默认' },
  ...countries.value,
])

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

async function addToLibrary(row: Models.app.CandidateRow) {
  try {
    const id = await App.AddToLibrary(row)
    row.localTrackId = id
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
    <header class="shrink-0">
      <PaneHeader>
      <div class="flex w-full items-center gap-3" style="--wails-draggable: no-drag">
        <div class="relative mx-auto w-full max-w-xl">
          <SearchIcon
            class="pointer-events-none absolute left-3 top-1/2 z-10 size-4 -translate-y-1/2 text-muted-color"
          />
          <InputText
            v-model="search.query"
            type="search"
            size="small"
            pt:root:class="w-full p-small:pl-9"
            :placeholder="search.isLibrary ? '搜索资料库' : `在 ${scopeLabel} 中搜索`"
            @keyup.enter="run"
          />
        </div>

        <AdvancedSearchPanel @run="run" />

        <!-- 范围：单选下拉，上游从注册表渲染 -->
        <Select
          :model-value="search.scope"
          :options="scopeOptions"
          option-label="label"
          option-value="id"
          size="small"
          class="shrink-0"
          @update:model-value="search.setScope($event)"
        >
          <template #option="{ option }">
            <span>{{ option.label }}</span>
            <span v-if="!option.configured" class="text-xs text-muted-color" :title="option.reason">
              （未配置）
            </span>
          </template>
        </Select>

        <Button
          size="small"
          class="shrink-0"
          :disabled="!canSearch"
          :title="!search.isLibrary && !settings.outboundAllowed ? settings.outboundReason : ''"
          @click="run"
        >
          {{ search.loading ? '搜索中…' : '搜索' }}
        </Button>
      </div>
      </PaneHeader>

      <!-- 地区下拉仅在当前上游需要地区参数时出现 -->
      <div v-if="showCountry" class="flex items-center justify-end gap-2 px-4 pb-2 text-xs">
        <label class="flex items-center gap-1">
          地区
          <Select
            v-model="search.country"
            :options="countryOptions"
            option-label="name"
            option-value="code"
            size="small"
            @update:model-value="countryTouched = true"
          />
        </label>
        <span v-if="hint?.detected" class="text-muted-color">{{ hint.message }}</span>
      </div>
    </header>

    <!-- 出网禁用时把原因说清楚，而不是让搜索按钮无声地灰着 -->
    <Message
      v-if="!search.isLibrary && !settings.outboundAllowed"
      severity="secondary"
      size="small"
      class="shrink-0 rounded-none"
    >
      <span class="flex-1">{{ settings.outboundReason }}</span>
      <SecondaryButton variant="outlined" size="small" @click="emit('goSettings')">
        前往设置
      </SecondaryButton>
    </Message>

    <div class="min-h-0 flex-1 overflow-auto">
      <!-- 资料库结果：与其他视图共用同一个曲目表格 -->
      <TrackTable
        :drawer-open="drawerOpen"
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

      <div v-else class="flex h-full flex-col items-center justify-center gap-2 text-muted-color">
        <SearchIcon class="size-12 opacity-40" />
        <p class="text-base font-semibold">
          {{ search.isLibrary ? '搜索你的资料库' : `在 ${scopeLabel} 中搜索` }}
        </p>
        <p class="text-xs">条件可任意留空；需要更精确时用「高级」添加条件</p>
      </div>
    </div>

    <Message v-if="search.error" severity="error" size="small" class="shrink-0 rounded-none">
      {{ search.error }}
    </Message>
  </div>
</template>
