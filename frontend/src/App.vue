<script setup lang="ts">
/**
 * 应用外壳。
 *
 * 没有顶栏：macOS 用 TitleBarHiddenInset 时红绿灯浮在内容之上，
 * 顶栏左侧放什么都会被压住。红绿灯的位置让给侧栏顶部，
 * 原顶栏上的三个入口各自归位——
 * 搜索去了搜索页与各视图右上角，导入去了导入队列页，主题切换去了设置。
 */
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { App as Api, Events, EventsOn, display } from '@/api'
import type { Models } from '@/api'
import AppSidebar from '@/components/AppSidebar.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PromptDialog from '@/components/PromptDialog.vue'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import type { SortOption } from '@/components/SortMenu.vue'
import ViewHeader from '@/components/ViewHeader.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import EmptyState from '@/components/EmptyState.vue'
import GlobalNotice from '@/components/GlobalNotice.vue'
import ArtistsView from '@/views/ArtistsView.vue'
import AlbumsView from '@/views/AlbumsView.vue'
import SearchView from '@/views/SearchView.vue'
import ImportQueueView from '@/views/ImportQueueView.vue'
import ReviewView from '@/views/ReviewView.vue'
import SettingsDialog from '@/views/SettingsDialog.vue'
import ImportWizard from '@/views/ImportWizard.vue'
import EditDialog from '@/views/EditDialog.vue'
import { useLibraryStore } from '@/stores/library'
import { useSearchStore } from '@/stores/search'
import { useSettingsStore } from '@/stores/settings'
import { useEnrichStore } from '@/stores/enrich'
import { useTasksStore } from '@/stores/tasks'
import { useThemeStore } from '@/stores/theme'
import { useUiStore } from '@/stores/ui'

const library = useLibraryStore()
const settings = useSettingsStore()
const tasks = useTasksStore()
const enrich = useEnrichStore()
const search = useSearchStore()
const ui = useUiStore()
useThemeStore()

const drawerOpen = ref(false)
const detail = ref<Models.main.TrackDetail | null>(null)
const editing = ref<number | null>(null)
const notice = ref('')
/** 离线态：在统一的全局提示位显示，页面内不重复弹出相同错误。 */
const offline = ref(false)
const selectedIds = ref<number[]>([])

/** 两栏页面的排序状态。就地排序，不为换个顺序再往返一次后端。 */
const sortOptions: SortOption[] = [
  { key: 'default', label: '默认顺序' },
  { key: 'title', label: '标题' },
  { key: 'artist', label: '艺术家' },
  { key: 'album', label: '专辑' },
  { key: 'year', label: '年份' },
  { key: 'duration', label: '时长' },
]
const sortKey = ref('default')
const sortDesc = ref(false)

/**
 * 排序维度与表格字段的对照。
 *
 * 排序菜单说的是「维度」，DataTable 认的是「字段名」，两边差一层：
 * 「默认顺序」对应没有字段（保持后端给的顺序，歌单的人工排序就在里面），
 * 「时长」对应的字段名是 durationMs。
 */
const SORT_FIELDS: Record<string, string | null> = {
  default: null,
  title: 'title',
  artist: 'artist',
  album: 'album',
  year: 'year',
  duration: 'durationMs',
}

/**
 * 双向：排序菜单与点击列头写的是同一份状态。
 *
 * 表格的列头本身也可点击排序，两个入口若各存一份状态，
 * 点完列头再打开菜单会看到一个过期的勾。
 */
const sortField = computed<string | null>({
  get: () => SORT_FIELDS[sortKey.value] ?? null,
  set: (field) => {
    const hit = Object.entries(SORT_FIELDS).find(([, f]) => f === field)
    sortKey.value = hit ? hit[0] : 'default'
  },
})
const sortOrder = computed<number>({
  get: () => (sortDesc.value ? -1 : 1),
  set: (order) => {
    sortDesc.value = order < 0
  },
})

let offLibrary: (() => void) | null = null

onMounted(async () => {
  await settings.load()
  await library.loadCollections()
  await library.loadTracks()
  tasks.subscribe()
  await tasks.refresh()
  // 补全信息匹配的状态：后端推送，这里只订阅并读一次初值
  enrich.subscribe()
  await enrich.refresh()
  offLibrary = EventsOn(Events.LibraryChanged, async () => {
    await library.loadCollections()
    await library.loadTracks()
    // 入库或补全会改变「已匹配/未匹配」的计数，工具栏上那个数字要跟着动
    await enrich.refresh()
  })

  // 启动时探一次连通性，之后只在出网请求失败时置位——
  // 不做后台轮询，离线提示靠请求结果驱动。
  try {
    offline.value = !(await Api.CheckReachable())
  } catch {
    offline.value = false
  }
})

onUnmounted(() => {
  tasks.unsubscribeAll()
  enrich.unsubscribeAll()
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
    coverSource: { provider: t.matchSource, ref: t.releaseRef, rg: t.releaseGroupRef },
    // 封面挂在歌集上：已采用的那张优先于临时缩略图
    adoptedCoverPath: t.coverPath,
  })),
)

const showTrackTable = computed(() =>
  ['all', 'recent', 'playlist', 'tag'].includes(library.view),
)

/** 歌单的拖拽重排只在默认顺序下可用——在排序视图里拖动，落点是没有意义的。 */
const canReorder = computed(() => library.view === 'playlist' && sortKey.value === 'default')

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
    case 'upstream': await goUpstreamSearch(); break
    case 'all': await library.goTo('all'); break
    case 'import': ui.importOpen = true; break
  }
}

/** 到上游搜索：搜索页 + 范围切到主上游。 */
async function goUpstreamSearch(prefill?: { title: string; artist: string; album: string }) {
  const provider = settings.providers[0]?.id ?? 'musicbrainz'
  if (prefill) search.prefillUpstream(provider, prefill)
  else search.setScope(provider)
  await library.goTo('search')
}

async function openDetail(row: Row) {
  try {
    detail.value = await Api.GetTrackDetail(row.id)
    drawerOpen.value = true
  } catch (e) {
    markFailure(e)
  }
}

/** 出网失败的统一归口：失败即在全局提示位显示，不各页各弹一份。 */
function markFailure(e: unknown) {
  const msg = String(e)
  if (/dial|timeout|no such host|connection refused|network|离线/i.test(msg)) {
    offline.value = true
  }
  notice.value = msg
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
  const label = kind === 'playlist' ? '歌单' : '标签'
  const name = await ui.prompt({
    title: `加入${label}`,
    description: `现有${label}：${list.map((c) => c.name).join('、') || '（无）'}`,
    label: `${label}名称（不存在则新建）`,
  })
  if (!name) return
  try {
    let id = list.find((c) => c.name === name)?.id
    if (!id) id = await Api.CreateCollection(name, kind, undefined)
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
  const ok = await ui.confirm({
    title: `删除 ${targets.length} 首曲目？`,
    message: '此操作不可撤销。',
    confirmLabel: '删除',
    danger: true,
  })
  if (!ok) return
  try {
    await Api.DeleteTracks(targets.map((t) => t.id))
    drawerOpen.value = false
    await library.loadTracks()
  } catch (e) {
    notice.value = String(e)
  }
}

/** 「查看上游」：以该曲的字段跳到搜索页的上游范围。 */
async function viewUpstream(row: Row) {
  await goUpstreamSearch({
    title: row.title,
    artist: row.artist ?? '',
    album: row.album ?? '',
  })
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

/** 采用封面后重新读一次详情，使抽屉立刻显示新图。 */
async function onCoverAdopted() {
  if (detail.value) {
    try {
      detail.value = await Api.GetTrackDetail(detail.value.track.id)
    } catch (e) {
      markFailure(e)
    }
  }
  await library.loadTracks()
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

async function newCollection(kind: 'playlist' | 'tag') {
  const label = kind === 'playlist' ? '歌单' : '标签'
  const name = await ui.prompt({ title: `新建${label}`, label: `${label}名称` })
  if (!name) return
  try {
    await Api.CreateCollection(name, kind, undefined)
    await library.loadCollections()
  } catch (e) {
    notice.value = String(e)
  }
}

async function renameCollection(id: number, kind: 'playlist' | 'tag') {
  const list = kind === 'playlist' ? library.playlists : library.tags
  const current = list.find((c) => c.id === id)
  const name = await ui.prompt({
    title: `重命名${kind === 'playlist' ? '歌单' : '标签'}`,
    label: '新名称',
    value: current?.name ?? '',
  })
  if (!name || name === current?.name) return
  try {
    await Api.RenameCollection(id, name)
    await library.loadCollections()
    if (library.contextId === id) library.contextTitle = name
  } catch (e) {
    notice.value = String(e)
  }
}

async function deleteCollection(id: number, kind: 'playlist' | 'tag') {
  const label = kind === 'playlist' ? '歌单' : '标签'
  const ok = await ui.confirm({
    title: `删除这个${label}？`,
    message: '曲目本身不会被删除。',
    confirmLabel: '删除',
    danger: true,
  })
  if (!ok) return
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
  <div class="flex h-full bg-app text-color">
    <AppSidebar
      @new-collection="newCollection"
      @rename-collection="renameCollection"
      @delete-collection="deleteCollection"
    />

    <div class="flex min-w-0 flex-1 flex-col">
      <!-- 全局提示位置：出网禁用、离线、出网失败统一在这里 -->
      <GlobalNotice
        :message="offline ? '' : notice"
        :offline="offline"
        @dismiss="notice = ''"
        @open-settings="ui.openSettings('upstream')"
      />

      <main class="flex min-h-0 min-w-0 flex-1 flex-col">
        <!-- 标准曲目表格：全部曲目 / 最近添加 / 歌单 / 标签 -->
        <template v-if="showTrackTable">
          <ViewHeader
            v-model:sort-key="sortKey"
            v-model:sort-desc="sortDesc"
            :sort-options="sortOptions"
          />
          <TrackTable
            v-if="rows.length > 0"
            v-model:sort-field="sortField"
            v-model:sort-order="sortOrder"
            :rows="rows"
            :collection-id="library.view === 'playlist' ? library.contextId : 0"
            :reorderable="canReorder"
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

        <SearchView
          v-else-if="library.view === 'search'"
          @open-track="openDetail"
          @edit-track="(r) => (editing = r.id)"
          @add-to-collection="addToCollection"
          @view-artist="viewArtist"
          @copy-info="copyInfo"
          @remove-tracks="removeTracks"
          @selection-change="(ids) => (selectedIds = ids)"
          @export-scope="exportScope"
          @jump-local="openTrackById"
          @go-settings="ui.openSettings('upstream')"
        />

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
          @add-to-collection="addToCollection"
          @view-artist="viewArtist"
          @copy-info="copyInfo"
          @remove-tracks="removeTracks"
          @selection-change="(ids) => (selectedIds = ids)"
          @export-scope="exportScope"
          @view-upstream="viewUpstream"
        />

        <ImportQueueView
          v-else-if="library.view === 'importQueue'"
          @go-review="library.goTo('review')"
        />

        <ReviewView
          v-else-if="library.view === 'review'"
          @jump-local="openTrackById"
          @go-settings="ui.openSettings('upstream')"
        />
      </main>
    </div>

    <DetailDrawer
      :detail="detail"
      :open="drawerOpen"
      @close="drawerOpen = false"
      @edit="(id) => (editing = id)"
      @view-artist="goToArtist"
      @cover-adopted="onCoverAdopted"
    />

    <ImportWizard
      v-if="ui.importOpen"
      @close="ui.importOpen = false"
      @submitted="tasks.refresh()"
      @go-settings="ui.openSettings('upstream')"
    />

    <EditDialog
      v-if="editing !== null"
      :track-id="editing"
      @close="editing = null"
      @saved="library.loadTracks()"
    />

    <SettingsDialog />
    <PromptDialog />
    <ConfirmDialog />
  </div>
</template>
