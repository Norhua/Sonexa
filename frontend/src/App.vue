<script setup lang="ts">
/**
 * 应用外壳。
 *
 * 没有顶栏：macOS 用 TitleBarHiddenInset 时红绿灯浮在内容之上，
 * 顶栏左侧放什么都会被压住。红绿灯的位置让给侧栏顶部，
 * 原顶栏上的三个入口各自归位——
 * 搜索去了搜索页与各视图右上角，导入去了导入队列页，主题切换去了设置。
 */
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { App as Api, Events, EventsOn, display } from '@/api'
import type { Models } from '@/api'
import AppSidebar from '@/components/AppSidebar.vue'
import ConfirmDialog from '@/components/ConfirmDialog.vue'
import PromptDialog from '@/components/PromptDialog.vue'
import TrackTable, { type Row } from '@/components/TrackTable.vue'
import ViewHeader from '@/components/ViewHeader.vue'
import DetailDrawer from '@/components/DetailDrawer.vue'
import EmptyState from '@/components/EmptyState.vue'
import GlobalNotice from '@/components/GlobalNotice.vue'
import ArtistsView from '@/views/ArtistsView.vue'
import AlbumsView from '@/views/AlbumsView.vue'
import SearchView from '@/views/SearchView.vue'
import ImportQueueView from '@/views/ImportQueueView.vue'
import ReviewView from '@/views/ReviewView.vue'
import CoverSearchView from '@/views/CoverSearchView.vue'
import { useCoverSearchStore } from '@/stores/coverSearch'
/** 模态表单按需载入；首次浏览不解析设置、导入与编辑的控件代码。 */
const SettingsDialog = defineAsyncComponent(() => import('@/views/SettingsDialog.vue'))
const ImportWizard = defineAsyncComponent(() => import('@/views/ImportWizard.vue'))
const EditDialog = defineAsyncComponent(() => import('@/views/EditDialog.vue'))
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
const coverSearch = useCoverSearchStore()
useThemeStore()

const drawerOpen = ref(false)
const detail = ref<Models.app.TrackDetail | null>(null)
const editing = ref<number | null>(null)
const notice = ref('')
/** 离线态：在统一的全局提示位显示，页面内不重复弹出相同错误。 */
const offline = ref(false)
const selectedIds = ref<number[]>([])

/** 两栏页面的排序状态。就地排序，不为换个顺序再往返一次后端。 */
const sortKey = ref('default')
const sortDesc = ref(false)

let offLibrary: (() => void) | null = null
let offCover: (() => void) | null = null
watch(() => [library.view, library.contextId], () => { if (coverSearch.target) void coverSearch.close() })

onMounted(async () => {
  await settings.load()
  await library.loadCollections()
  await library.loadTracks()
  tasks.subscribe()
  await tasks.refresh()
  // 补全信息匹配的状态：后端推送，这里只订阅并读一次初值
  enrich.subscribe()
  await enrich.refresh()
  offCover = EventsOn(Events.CoverChanged, async (event: { releaseId: number }) => {
    if (detail.value?.track.releaseId === event.releaseId && drawerOpen.value) await onCoverAdopted()
  })
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
  offCover?.()
  void coverSearch.close()
})

/** 把后端的曲目视图转为表格行。 */
const rows = computed<Row[]>(() =>
  library.tracks.map((t) => ({
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
    coverSource: { releaseId: t.releaseId, revision: t.coverRevision, provider: t.matchSource, ref: t.releaseRef, rg: t.releaseGroupRef },
    // 封面挂在歌集上：已采用的那张优先于临时缩略图
    adoptedCoverPath: t.coverPath,
  })),
)

const showTrackTable = computed(() =>
  ['all', 'recent', 'playlist', 'tag'].includes(library.view),
)

const tableContext = computed(() => `${library.view}:${library.contextId}`)
watch(tableContext, () => { selectedIds.value = [] })

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

let detailRequest = 0
watch(drawerOpen, open => {
  if (!open) {
    detailRequest++
    if (coverSearch.target) void coverSearch.close()
  }
}, { flush: 'sync' })
watch(() => detail.value?.track.id, id => {
  if (coverSearch.target && coverSearch.target.track.id !== id) void coverSearch.close()
})
async function openDetail(row: Row) {
  await openTrackById(row.id)
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
  const request = ++detailRequest
  try {
    const next = await Api.GetTrackDetail(trackId)
    if (request !== detailRequest) return
    detail.value = next
    drawerOpen.value = true
  } catch (e) {
    if (request === detailRequest) markFailure(e)
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
  if (detail.value && drawerOpen.value) await openTrackById(detail.value.track.id)
  await library.loadTracks()
}

async function goToArtist(artistId: number) {
  try {
    drawerOpen.value = false
    // 立即导航；ArtistsView 按当前上下文读取分面，并负责丢弃旧响应。
    await library.goTo('artist', { id: artistId, title: '艺术家' })
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
    const searchResult = scope === 'filtered' && library.view === 'search' && search.isLibrary
    const res = await Api.ExportTracks({
      // 搜索页不使用 library.baseFilter：那是上一个浏览视图的过滤器。
      // 导出已展示结果的快照，不能把全库误当成「当前列表」。
      scope: searchResult ? 'selection' : scope,
      format: settings.settings?.importExport.exportFormat ?? 'csv',
      depth: 'full',
      filter: library.baseFilter(),
      trackIds: searchResult ? search.libraryRows.map(row => row.id) : selectedIds.value,
    } as Models.app.ExportRequest)
    if (res) notice.value = `已导出 ${res.count} 条到 ${res.path}`
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
    else await library.loadTracks()
  } catch (e) {
    notice.value = String(e)
  }
}
</script>

<template>
  <div class="flex h-full bg-app text-color" :inert="!!coverSearch.target">
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
          <ViewHeader />
          <TrackTable
            :drawer-open="drawerOpen"
            v-if="rows.length > 0"
            v-model:sort-key="sortKey"
            v-model:sort-desc="sortDesc"
            :context-key="tableContext"
            :rows="rows"
            :collection-id="library.view === 'playlist' ? library.contextId : 0"
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
          :drawer-open="drawerOpen"
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
            :drawer-open="drawerOpen"
            :context-key="tableContext"
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
          :drawer-open="drawerOpen"
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
      :key="detail?.track.id"
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

    <CoverSearchView v-if="coverSearch.target" />
    <SettingsDialog v-if="ui.settingsOpen" />
    <PromptDialog />
    <ConfirmDialog />
  </div>
</template>
