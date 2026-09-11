<script setup lang="ts">
/**
 * 应用外壳。
 *
 * 顶栏（快速搜索 + 高级 + 设置）、侧栏四分组、主内容区、详情抽屉。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { Moon, Sun, Music4, Upload } from 'lucide-vue-next'
import { App as Api, Events, EventsOn, display } from '@/api'
import type { Models } from '@/api'
import AppSidebar from '@/components/AppSidebar.vue'
import SearchBar from '@/components/SearchBar.vue'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import EmptyState from '@/components/EmptyState.vue'
import GlobalNotice from '@/components/GlobalNotice.vue'
import ArtistsView from '@/views/ArtistsView.vue'
import AlbumsView from '@/views/AlbumsView.vue'
import UpstreamSearchView from '@/views/UpstreamSearchView.vue'
import ReviewView from '@/views/ReviewView.vue'
import SettingsView from '@/views/SettingsView.vue'
import ImportWizard from '@/views/ImportWizard.vue'
import EditDialog from '@/views/EditDialog.vue'
import { useLibraryStore } from '@/stores/library'
import { useSettingsStore } from '@/stores/settings'
import { useTasksStore } from '@/stores/tasks'
import { useThemeStore } from '@/stores/theme'

const library = useLibraryStore()
const settings = useSettingsStore()
const tasks = useTasksStore()
const theme = useThemeStore()

const drawerOpen = ref(false)
const detail = ref<Models.main.TrackDetail | null>(null)
const editing = ref<number | null>(null)
const importOpen = ref(false)
const notice = ref('')
const upstreamPrefill = ref<{ title: string; artist: string; album: string } | undefined>()
const selectedIds = ref<number[]>([])

let offLibrary: (() => void) | null = null

onMounted(async () => {
  await settings.load()
  await library.loadCollections()
  await library.loadTracks()
  tasks.subscribe()
  await tasks.refresh()
  offLibrary = EventsOn(Events.LibraryChanged, async () => {
    await library.loadCollections()
    await library.loadTracks()
  })
})

onUnmounted(() => {
  tasks.unsubscribeAll()
  offLibrary?.()
})

/** 把后端的曲目视图转为表格行。 */
const rows = computed<Row[]>(() =>
  library.tracks.map((t) => ({
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

const showTrackTable = computed(() =>
  ['all', 'recent', 'playlist', 'tag'].includes(library.view),
)

/** 空态文案各不相同，不复用同一句通用提示。 */
const emptyState = computed(() => {
  if (library.chips.length > 0) {
    return {
      title: '没有匹配的曲目',
      description: '当前筛选条件下没有命中任何曲目。',
      actions: [
        { label: '清除筛选', key: 'clear' },
        { label: '到上游搜索', key: 'upstream' },
      ],
    }
  }
  if (library.view === 'playlist' || library.view === 'tag') {
    return {
      title: `这个${library.view === 'tag' ? '标签' : '歌单'}还是空的`,
      description: '在「全部曲目」中多选曲目，再从右键菜单加入。',
      actions: [{ label: '到全部曲目', key: 'all' }],
    }
  }
  return {
    title: '曲库里还没有曲目',
    description: '从表格文件导入，或到上游搜索后加入曲库。',
    actions: [
      { label: '导入表格', key: 'import' },
      { label: '到上游搜索', key: 'upstream' },
    ],
  }
})

async function onEmptyAction(key: string) {
  switch (key) {
    case 'clear': await library.clearAllFilters(); break
    case 'upstream': await library.goTo('upstream'); break
    case 'all': await library.goTo('all'); break
    case 'import': importOpen.value = true; break
  }
}

async function openDetail(row: Row) {
  try {
    detail.value = await Api.GetTrackDetail(row.id)
    drawerOpen.value = true
  } catch (e) {
    notice.value = String(e)
  }
}

async function openTrackById(trackId: number) {
  try {
    detail.value = await Api.GetTrackDetail(trackId)
    drawerOpen.value = true
  } catch (e) {
    notice.value = String(e)
  }
}

async function addToCollection(targets: Row[], kind: 'playlist' | 'tag') {
  const list = kind === 'playlist' ? library.playlists : library.tags
  const name = window.prompt(
    `加入哪个${kind === 'playlist' ? '歌单' : '标签'}？（输入名称，不存在则新建）\n现有：${list.map((c) => c.name).join('、') || '（无）'}`,
  )
  if (!name?.trim()) return
  try {
    let target = list.find((c) => c.name === name.trim())
    let id = target?.id
    if (!id) id = await Api.CreateCollection(name.trim(), kind, undefined)
    await Api.AddToCollection(id, targets.map((t) => t.id))
    await library.loadCollections()
  } catch (e) {
    notice.value = String(e)
  }
}

async function removeFromCollection(targets: Row[]) {
  try {
    await Api.RemoveFromCollection(library.contextId, targets.map((t) => t.id))
    await library.loadTracks()
    await library.loadCollections()
  } catch (e) {
    notice.value = String(e)
  }
}

async function removeTracks(targets: Row[]) {
  if (!window.confirm(`删除 ${targets.length} 首曲目？此操作不可撤销。`)) return
  try {
    await Api.DeleteTracks(targets.map((t) => t.id))
    drawerOpen.value = false
    await library.loadTracks()
  } catch (e) {
    notice.value = String(e)
  }
}

async function viewArtist(row: Row) {
  try {
    const d = await Api.GetTrackDetail(row.id)
    const first = d.participants[0]?.artists[0] ?? d.creditSpans[0]
    if (!first) return
    const artistId = 'artistId' in first ? first.artistId : 0
    if (artistId) await goToArtist(artistId)
  } catch (e) {
    notice.value = String(e)
  }
}

async function goToArtist(artistId: number) {
  try {
    const header = await Api.GetArtistHeader(artistId)
    drawerOpen.value = false
    await library.goTo('artist', { id: artistId, title: header.title })
  } catch (e) {
    notice.value = String(e)
  }
}

function copyInfo(targets: Row[]) {
  const text = targets
    .map((t) => `${t.title}\t${display(t.artist)}\t${display(t.album)}`)
    .join('\n')
  void navigator.clipboard?.writeText(text)
}

async function exportScope(scope: 'playlist' | 'filtered' | 'selection' | 'all') {
  try {
    const res = await Api.ExportTracks({
      scope,
      format: settings.settings?.importExport.exportFormat ?? 'csv',
      depth: 'full',
      filter: library.baseFilter(),
      trackIds: selectedIds.value,
    } as Models.main.ExportRequest)
    if (res) notice.value = `已导出 ${res.count} 条到 ${res.path}`
  } catch (e) {
    notice.value = String(e)
  }
}

/** 歌单拖拽重排：持久化新顺序，重开该歌单时保持。 */
async function reorderPlaylist(orderedIds: number[]) {
  if (library.view !== 'playlist') return
  try {
    await Api.ReorderCollection(library.contextId, orderedIds)
    await library.loadTracks()
  } catch (e) {
    notice.value = String(e)
  }
}

async function toUpstream(fields: { title: string; artist: string; album: string }) {
  upstreamPrefill.value = fields
  await library.goTo('upstream')
}

async function newCollection(kind: 'playlist' | 'tag') {
  const name = window.prompt(`新建${kind === 'playlist' ? '歌单' : '标签'}名称`)
  if (!name?.trim()) return
  try {
    await Api.CreateCollection(name.trim(), kind, undefined)
    await library.loadCollections()
  } catch (e) {
    notice.value = String(e)
  }
}

async function renameCollection(id: number, _kind: 'playlist' | 'tag', name: string) {
  try {
    await Api.RenameCollection(id, name)
    await library.loadCollections()
  } catch (e) {
    notice.value = String(e)
  }
}

async function deleteCollection(id: number, kind: 'playlist' | 'tag') {
  if (!window.confirm(`删除这个${kind === 'playlist' ? '歌单' : '标签'}？曲目本身不会被删除。`)) return
  try {
    await Api.DeleteCollection(id)
    await library.loadCollections()
    if (library.contextId === id) await library.goTo('all')
  } catch (e) {
    notice.value = String(e)
  }
}
</script>

<template>
  <div class="flex h-full flex-col bg-background text-foreground">
    <!-- 顶栏 -->
    <header class="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <Music4 class="size-5 shrink-0" />
      <span class="shrink-0 font-semibold">Sonexa</span>
      <SearchBar class="max-w-2xl" @to-upstream="toUpstream" />
      <button
        class="flex shrink-0 items-center gap-1 rounded-md border px-2 py-1.5 text-sm hover:bg-accent disabled:opacity-50"
        :disabled="!settings.outboundAllowed"
        :title="!settings.outboundAllowed ? settings.outboundReason : '导入表格'"
        @click="importOpen = true"
      >
        <Upload class="size-4" />
        导入
      </button>
      <button
        class="shrink-0 rounded-md p-1.5 hover:bg-accent"
        :title="theme.theme === 'dark' ? '切换到亮色' : '切换到暗色'"
        @click="theme.toggle()"
      >
        <Moon v-if="theme.theme === 'dark'" class="size-4" />
        <Sun v-else class="size-4" />
      </button>
    </header>

    <!-- 全局提示位置：出网禁用、离线、出网失败统一在这里 -->
    <GlobalNotice
      :message="notice"
      @dismiss="notice = ''"
      @open-settings="library.goTo('settings')"
    />

    <div class="flex min-h-0 flex-1">
      <AppSidebar
        @new-collection="newCollection"
        @rename-collection="renameCollection"
        @delete-collection="deleteCollection"
      />

      <main class="flex min-w-0 flex-1 flex-col">
        <!-- 标准曲目表格：全部曲目 / 最近添加 / 歌单 / 标签 -->
        <template v-if="showTrackTable">
          <div class="shrink-0 border-b px-4 py-2">
            <h2 class="text-base font-semibold">{{ library.contextTitle }}</h2>
          </div>
          <TrackTable
            v-if="rows.length > 0"
            :rows="rows"
            :collection-id="library.view === 'playlist' ? library.contextId : 0"
            :reorderable="library.view === 'playlist'"
            :loading="library.loading"
            @open="openDetail"
            @edit="(r) => (editing = r.id)"
            @add-to-collection="addToCollection"
            @remove-from-collection="removeFromCollection"
            @remove="removeTracks"
            @view-artist="viewArtist"
            @copy-info="copyInfo"
            @selection-change="(ids) => (selectedIds = ids)"
            @export-scope="exportScope"
            @reorder="reorderPlaylist"
          />
          <EmptyState
            v-else-if="!library.loading"
            :title="emptyState.title"
            :description="emptyState.description"
            :actions="emptyState.actions"
            @action="onEmptyAction"
          />
        </template>

        <!-- 艺术家视图：中列 + 按 artist_id 过滤的标准曲目表格 -->
        <ArtistsView v-else-if="library.view === 'artists' || library.view === 'artist'">
          <TrackTable
            :rows="rows"
            :loading="library.loading"
            @open="openDetail"
            @edit="(r) => (editing = r.id)"
            @add-to-collection="addToCollection"
            @remove="removeTracks"
            @view-artist="viewArtist"
            @copy-info="copyInfo"
            @selection-change="(ids) => (selectedIds = ids)"
            @export-scope="exportScope"
          />
        </ArtistsView>

        <AlbumsView
          v-else-if="library.view === 'albums' || library.view === 'album'"
          @open-track="openTrackById"
          @edit-track="(id) => (editing = id)"
        />

        <UpstreamSearchView
          v-else-if="library.view === 'upstream'"
          :prefill="upstreamPrefill"
          @jump-local="openTrackById"
        />

        <ReviewView
          v-else-if="library.view === 'review'"
          @jump-local="openTrackById"
          @go-settings="library.goTo('settings')"
        />

        <SettingsView v-else-if="library.view === 'settings'" />
      </main>

      <DetailDrawer
        :detail="detail"
        :open="drawerOpen"
        @close="drawerOpen = false"
        @edit="(id) => (editing = id)"
        @view-artist="goToArtist"
      />
    </div>

    <ImportWizard
      v-if="importOpen"
      @close="importOpen = false"
      @submitted="tasks.refresh()"
    />

    <EditDialog
      v-if="editing !== null"
      :track-id="editing"
      @close="editing = null"
      @saved="library.loadTracks()"
    />
  </div>
</template>
