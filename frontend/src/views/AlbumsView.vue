<script setup lang="ts">
import { useAdmissionStore } from '@/stores/admission'
const admission = useAdmissionStore()
/**
 * 专辑浏览。
 *
 * 与艺术家视图不同构——它要显示用户**没有**的曲目（灰色行）。
 * 至少拥有一首才会出现在列表里：灰色行是「补全你已开始收藏的专辑」，
 * 不是上游目录浏览器。
 */
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { App, display, Events, EventsOn, type Models } from '@/api'
import type { Row } from '@/components/TrackTable.vue'
import CoverArt from '@/components/CoverArt.vue'
import GroupProposals from '@/components/GroupProposals.vue'
import PaneHeader from '@/components/PaneHeader.vue'
import ViewOptionsMenu from '@/components/ViewOptionsMenu.vue'
import FilterChips from '@/components/FilterChips.vue'
import TrackTable from '@/components/TrackTable.vue'
import ViewSearchBox from '@/components/ViewSearchBox.vue'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useLibraryStore } from '@/stores/library'

defineProps<{ drawerOpen?: boolean }>()
const library = useLibraryStore()
const albums = ref<Models.store.AlbumListItem[]>([])
const detail = ref<Models.app.AlbumGroupDetail | null>(null)
const expanded = ref(new Set<number>())
const fetching = ref(new Set<number>())
const selected = ref(new Map<number, number[]>())
const loading = ref(false)
const error = ref('')

/** 「仅显示已有」开关的状态被记住。 */
const onlyOwned = ref(localStorage.getItem('sonexa.onlyOwned') === '1')
/** 中列的就地过滤：这一列是专辑名单，不是曲目，因此不走曲目筛选。 */
const filter = ref('')

const visibleAlbums = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const list = q
    ? albums.value.filter(
        (a) =>
          (a.title ?? '').toLowerCase().includes(q) || (a.credit ?? '').toLowerCase().includes(q),
      )
    : [...albums.value]

  return list.sort((a, b) => (a.title ?? '').localeCompare(b.title ?? '', 'zh-Hans-CN'))
})

const emit = defineEmits<{
  openTrack: [trackId: number]
  editTrack: [trackId: number]
  addToCollection: [rows: Row[], kind: 'playlist' | 'tag']
  viewArtist: [row: Row]
  copyInfo: [rows: Row[]]
  removeTracks: [rows: Row[]]
  viewUpstream: [row: Row]
  exportScope: [scope: 'playlist' | 'filtered' | 'selection' | 'all']
  selectionChange: [ids: number[]]
}>()

let listRequest = 0
let detailRequest = 0
let selectionRequest = 0
const offCover = EventsOn(Events.CoverChanged, async () => { await load(); await reloadDetail() })
const offLibrary = EventsOn(Events.LibraryChanged, async () => { await load(); await reloadDetail(); await library.loadTracks() })
onBeforeUnmount(() => { listRequest++; detailRequest++; selectionRequest++; offCover(); offLibrary() })
watch(() => library.filter.tagId, load, { immediate: true })

async function load() {
  const request = ++listRequest
  loading.value = true
  error.value = ''
  try {
    const tagId = library.filter.tagId
    const result = tagId ? await App.ListAlbumsForTag(tagId) : await App.ListAlbums()
    if (request === listRequest) albums.value = result ?? []
  } catch (e) { if (request === listRequest) error.value = String(e) }
  finally { if (request === listRequest) loading.value = false }
}
async function reloadDetail() {
  const groupId = detail.value?.groupId
  if (!groupId) return
  const request = ++detailRequest
  try {
    const next = await App.GetAlbumGroupDetail(groupId)
    if (request === detailRequest && detail.value?.groupId === groupId) detail.value = next
  } catch (e) { if (request === detailRequest) error.value = String(e) }
}
async function open(a: Models.store.AlbumListItem) {
  const request = ++selectionRequest
  ++detailRequest
  error.value = ''
  selected.value.clear()
  emit('selectionChange', [])
  try {
    const next = await App.GetAlbumGroupDetail(a.groupId)
    if (request !== selectionRequest) return
    detail.value = next
    expanded.value = new Set([next.representativeId])
    await library.goTo('album', { id: a.releaseId, groupId: a.groupId, title: a.title || '未命名专辑' })
    if (request !== selectionRequest) return
    const representative = next.releases.find(r => r.releaseId === next.representativeId)
    if (representative?.incomplete) void completeTracklist(representative, false)
  } catch (e) { if (request === selectionRequest) error.value = String(e) }
}
function coverSourceOf(a: { releaseId: number; coverRevision?: number }) {
  return { releaseId: a.releaseId, revision: a.coverRevision }
}
function albumTypeLabel(t?: string | null) {
  return t ? ({ album: '专辑', ep: 'EP', single: '单曲', broadcast: '广播', other: '其他' }[t] ?? t) : null
}
async function completeTracklist(release: Models.app.AlbumDetail, force: boolean) {
  if (!release.provider || !release.releaseRef || fetching.value.has(release.releaseId)) return
  fetching.value.add(release.releaseId)
  try { await App.FetchTracklist(release.provider, release.releaseRef, force); await reloadDetail(); await load() }
  catch (e) { error.value = String(e) }
  finally { fetching.value.delete(release.releaseId) }
}
function toggleRelease(release: Models.app.AlbumDetail) {
  if (expanded.value.has(release.releaseId)) expanded.value.delete(release.releaseId)
  else { expanded.value.add(release.releaseId); if (release.incomplete) void completeTracklist(release, false) }
}
function toggleOnlyOwned() {
  onlyOwned.value = !onlyOwned.value
  localStorage.setItem('sonexa.onlyOwned', onlyOwned.value ? '1' : '0')
}
function toRows(release: Models.app.AlbumDetail): Row[] {
  const taggedIds = new Set(library.tracks.map(track => track.id))
  return release.rows.filter(r => (!r.ghost || (!onlyOwned.value && expanded.value.has(release.releaseId))) &&
    (!library.filter.tagId || (!r.ghost && taggedIds.has(r.trackId))))
    .map(r => ({ id: r.trackId, rowKey: r.ghost ? `release:${release.releaseId}:${r.discNumber}:${r.trackNumber}:${r.trackRef ?? ''}` : `track:${r.trackId}`,
      title: r.title, artist: r.credit, album: release.title, durationMs: r.durationMs, addedAt: r.addedAt,
      trackNumber: r.trackNumber || null, discNumber: r.discNumber || null, ghost: r.ghost, trackRef: r.trackRef,
      recordingRef: r.recordingRef, coverSource: coverSourceOf(release), adoptedCoverPath: release.coverPath ?? null }))
}
async function addGhost(release: Models.app.AlbumDetail, row: Row) {
  if (!row.trackRef) return
  try { const result = await admission.run(() => App.PrepareGhostAdmission(release.releaseId, row.discNumber ?? 0, row.trackNumber ?? 0)); if (!result) return; await reloadDetail(); await load(); await library.loadTracks() }
  catch (e) { error.value = String(e) }
}
function selectionChanged(id: number, ids: number[]) {
  selected.value.set(id, ids)
  emit('selectionChange', [...selected.value.values()].flat())
}
</script>

<template>
  <div class="flex min-h-0 flex-1">
    <div class="flex w-64 shrink-0 flex-col border-r">
      <PaneHeader><h2 class="text-sm font-normal text-muted-color">专辑</h2></PaneHeader>
      <div class="min-h-0 flex-1 overflow-auto">
        <button v-for="album in visibleAlbums" :key="album.groupId"
          :class="['flex w-full items-center gap-2 px-3 py-2 text-left text-sm', detail?.groupId === album.groupId ? 'bg-highlight' : 'hover:bg-emphasis']" @click="open(album)">
          <CoverArt :source="coverSourceOf(album)" usage="thumbnail" class="size-10 shrink-0 rounded" icon-class="size-4" />
          <div class="min-w-0 flex-1">
            <p class="truncate font-medium">{{ album.title || '未命名专辑' }}</p>
            <p class="truncate text-xs text-muted-color">{{ display(album.credit) }}</p>
            <p class="text-xs text-muted-color">收藏 {{ album.ownedCount }} 首 · {{ album.memberCount }} 个在库版本</p>
          </div>
        </button>
        <p v-if="!visibleAlbums.length && !loading" class="px-3 py-4 text-sm text-muted-color">{{ filter || library.filter.tagId ? '没有匹配的专辑' : '还没有专辑' }}</p>
      </div>
    </div>
    <div class="flex min-w-0 flex-1 flex-col">
      <PaneHeader><div class="flex-1" /><ViewOptionsMenu /><ViewSearchBox v-model="filter" placeholder="在专辑中查找" /></PaneHeader>
      <FilterChips />
      <GroupProposals />
      <template v-if="detail">
        <div class="flex shrink-0 items-center gap-3 px-4 pb-3">
          <div class="min-w-0 flex-1">
            <h3 class="text-base font-semibold">{{ detail.title || '未命名专辑' }}</h3>
            <p class="text-xs text-muted-color">收藏 {{ detail.ownedCount }} 首 · {{ detail.memberCount }} 个在库版本（包含已保存空发行）</p>
          </div>
          <SecondaryButton size="small" :class="onlyOwned ? 'bg-highlight!' : ''" @click="toggleOnlyOwned">仅显示已有</SecondaryButton>
        </div>
        <div class="min-h-0 flex-1 overflow-auto">
          <section v-for="release in detail.releases" :key="release.releaseId" :data-release-id="release.releaseId" class="border-t">
            <div class="flex items-start gap-3 px-4 py-3">
              <CoverArt :source="coverSourceOf(release)" usage="detail" class="size-16 shrink-0 rounded" icon-class="size-5" />
              <div class="min-w-0 flex-1">
                <h4 class="font-medium">{{ release.title || '未命名发行' }} <span class="text-xs text-muted-color">{{ release.provider || '本地' }}</span></h4>
                <p class="text-xs text-muted-color">{{ display(release.credit) }}</p>
                <p class="text-xs text-muted-color">{{ display(albumTypeLabel(release.primaryType)) }} · {{ display(release.year) }} · {{ display(release.mediumFormat) }} · {{ display(release.country) }}</p>
                <p class="text-xs text-muted-color">收藏 {{ release.ownedCount }} 首 · 发行总数 {{ display(release.totalCount) }}<template v-if="release.discTotal"> · {{ release.discTotal }} 碟</template></p>
                <p v-if="release.media?.length" class="text-xs text-muted-color">{{ release.media.map(m => `第 ${m.number} 碟 ${display(m.trackTotal)} 首`).join(' · ') }}</p>
                <p v-if="release.label || release.catalogNumber" class="text-xs text-muted-color">{{ display(release.label) }} · {{ display(release.catalogNumber) }}</p>
                <p v-if="release.releaseCount" class="text-xs text-muted-color">上游组版本数 {{ release.releaseCount }}</p>
                <p v-if="release.rows.some(r => r.outsideTracklist)" class="text-xs text-muted-color">含本发行清单外收藏</p>
              </div>
              <SecondaryButton v-if="release.releaseRef" size="small" variant="text" @click="toggleRelease(release)">{{ expanded.has(release.releaseId) ? '收起清单' : '展开发行清单' }}</SecondaryButton>
            </div>
            <div v-if="toRows(release).length" :style="{ height: `${Math.min(520, 120 + toRows(release).length * 44)}px` }">
              <TrackTable :drawer-open="drawerOpen" :rows="toRows(release)" :loading="library.loading"
                :context-key="`album:${release.releaseId}`" column-context="album" fixed-order
                @open="r => emit('openTrack', r.id)" @edit="r => emit('editTrack', r.id)"
                @add-ghost-to-library="r => addGhost(release, r)"
                @add-to-collection="(rows, kind) => emit('addToCollection', rows, kind)"
                @view-artist="r => emit('viewArtist', r)" @copy-info="rows => emit('copyInfo', rows)"
                @remove="rows => emit('removeTracks', rows)" @view-upstream="r => emit('viewUpstream', r)"
                @export-scope="scope => emit('exportScope', scope)" @selection-change="ids => selectionChanged(release.releaseId, ids)" />
            </div>
            <p v-else class="px-4 pb-3 text-xs text-muted-color">{{ library.filter.tagId ? '此发行没有符合标签的收藏' : '此发行暂无收藏' }}</p>
            <div v-if="expanded.has(release.releaseId) && release.releaseRef" class="flex items-center gap-2 px-4 pb-3 text-xs text-muted-color">
              <span>{{ release.incomplete ? (release.tracklistError || '清单不完整') : '清单完整' }}</span>
              <span v-if="release.fetchedAt">更新于 {{ new Date(release.fetchedAt).toLocaleDateString() }}</span>
              <SecondaryButton size="small" variant="text" :disabled="fetching.has(release.releaseId)" @click="completeTracklist(release, true)">{{ fetching.has(release.releaseId) ? '获取中…' : release.incomplete ? '补全清单' : '重新获取' }}</SecondaryButton>
            </div>
          </section>
        </div>
      </template>
      <div v-else class="flex flex-1 items-center justify-center text-sm text-muted-color">从左侧选择一张专辑</div>
      <Message v-if="error || library.error" severity="error" size="small" class="shrink-0 rounded-none">{{ error || library.error }}</Message>
    </div>
  </div>
</template>
