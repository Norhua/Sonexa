<script setup lang="ts">
/**
 * 专辑浏览。
 *
 * 与艺术家视图不同构——它要显示用户**没有**的曲目（灰色行）。
 * 至少拥有一首才会出现在列表里：灰色行是「补全你已开始收藏的专辑」，
 * 不是上游目录浏览器。
 */
import { computed, onMounted, ref } from 'vue'
import { App, COVER_THUMB_SIZE, display, type Models } from '@/api'
import type { Row } from '@/components/TrackTable.vue'
import CoverArt from '@/components/CoverArt.vue'
import PaneHeader from '@/components/PaneHeader.vue'
import SortMenu, { type SortOption } from '@/components/SortMenu.vue'
import TrackTable from '@/components/TrackTable.vue'
import ViewSearchBox from '@/components/ViewSearchBox.vue'
import Message from '@/volt/Message.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useLibraryStore } from '@/stores/library'

const library = useLibraryStore()
const albums = ref<Models.store.AlbumListItem[]>([])
const detail = ref<Models.main.AlbumDetail | null>(null)
const loading = ref(false)
const fetching = ref(false)
const error = ref('')

/** 「仅显示已有」开关的状态被记住。 */
const onlyOwned = ref(localStorage.getItem('sonexa.onlyOwned') === '1')
/** 中列的就地过滤：这一列是专辑名单，不是曲目，因此不走曲目筛选。 */
const filter = ref('')

/** 中间栏的排序维度。 */
const sortOptions: SortOption[] = [
  { key: 'title', label: '专辑名' },
  { key: 'credit', label: '艺术家' },
  { key: 'owned', label: '已有曲目数' },
]
const sortKey = ref('title')
const sortDesc = ref(false)

const visibleAlbums = computed(() => {
  const q = filter.value.trim().toLowerCase()
  const list = q
    ? albums.value.filter(
        (a) =>
          (a.title ?? '').toLowerCase().includes(q) || (a.credit ?? '').toLowerCase().includes(q),
      )
    : [...albums.value]

  const dir = sortDesc.value ? -1 : 1
  return list.sort((a, b) => {
    switch (sortKey.value) {
      case 'owned':
        return dir * (a.ownedCount - b.ownedCount)
      case 'credit':
        return dir * (a.credit ?? '').localeCompare(b.credit ?? '', 'zh-Hans-CN')
      default:
        return dir * (a.title ?? '').localeCompare(b.title ?? '', 'zh-Hans-CN')
    }
  })
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

onMounted(load)

async function load() {
  loading.value = true
  try {
    albums.value = (await App.ListAlbums()) ?? []
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

/** 计数呈现：派生分组没有上游事实，呈现为 `3/-`。 */
function countLabel(a: Models.store.AlbumListItem): string {
  return a.totalCount === undefined || a.totalCount === null
    ? `${a.ownedCount}/-`
    : `${a.ownedCount}/${a.totalCount}`
}

async function open(a: Models.store.AlbumListItem) {
  error.value = ''
  detail.value = await App.GetAlbumDetail(a.releaseId)
  library.goTo('album', {
    id: a.releaseId,
    groupId: a.groupId,
    title: a.title || '未命名专辑',
  })

  // 首次打开且清单状态不是完整时补拉一次——不阻塞渲染：
  // 已入库曲目已经显示，灰色行随后补入。
  if (
    detail.value &&
    detail.value.tracklistState !== 'complete' &&
    detail.value.provider &&
    detail.value.releaseRef
  ) {
    void completeTracklist(false)
  }
}

/**
 * 一条专辑的封面来源。
 *
 * 封面是**歌集**的属性：同一张专辑的每一行、中列那一项、详情头部
 * 用的是同一张图。本地歌集没有上游标识，返回 null 由 CoverArt 呈现缺省态。
 */
function coverSourceOf(a: {
  provider?: string
  releaseGroupRef?: string
  representativeRef?: string
  releaseRef?: string
}): { provider?: string | null; ref?: string | null; rg?: string | null } | null {
  const ref = a.representativeRef ?? a.releaseRef
  if (!a.provider || !ref) return null
  return { provider: a.provider, ref, rg: a.releaseGroupRef ?? null }
}

/** 歌集类型的中文呈现。 */
function albumTypeLabel(t: string | null | undefined): string | null {
  if (!t) return null
  const labels: Record<string, string> = {
    album: '专辑', ep: 'EP', single: '单曲', broadcast: 'Broadcast', other: 'Other',
  }
  return labels[t] ?? t
}

async function completeTracklist(force: boolean) {
  if (!detail.value?.provider || !detail.value?.releaseRef) return
  fetching.value = true
  try {
    await App.FetchTracklist(detail.value.provider, detail.value.releaseRef, force)
    detail.value = await App.GetAlbumDetail(detail.value.releaseId)
    await load()
  } catch (e) {
    error.value = String(e)
  } finally {
    fetching.value = false
  }
}

function toggleOnlyOwned() {
  onlyOwned.value = !onlyOwned.value
  localStorage.setItem('sonexa.onlyOwned', onlyOwned.value ? '1' : '0')
}

/** 把专辑详情的行转为曲目表格的行。灰色行与普通行共用同一组件。 */
function toRows(): Row[] {
  if (!detail.value) return []
  return detail.value.rows
    .filter((r) => !onlyOwned.value || !r.ghost)
    .map((r) => ({
      // 「其他版本」行的 trackId 就是被命中的那首已有曲目，
      // 因此点击它能直接跳到用户手上的那一首。
      id: r.trackId,
      title: r.title,
      artist: r.credit,
      album: detail.value?.title,
      durationMs: r.durationMs,
      trackNumber: r.trackNumber,
      discNumber: r.discNumber,
      ghost: r.ghost,
      otherVersion: r.state === 'other_version',
      trackRef: r.trackRef,
      recordingRef: r.recordingRef,
      // 同一张专辑的每一行共用歌集封面——封面是歌集的属性
      coverSource: detail.value ? coverSourceOf(detail.value) : null,
      adoptedCoverPath: detail.value?.coverPath ?? null,
    }))
}

async function addGhost(row: Row) {
  if (!detail.value?.provider || !row.trackRef) return
  try {
    // 清单里已有 track 标识，直接带完整溯源入库——不需要再搜一次
    await App.AddGhostRowToLibrary(
      detail.value.provider, row.trackRef, row.title,
      row.artist ?? undefined, row.durationMs ?? undefined, row.recordingRef ?? undefined,
    )
    detail.value = await App.GetAlbumDetail(detail.value.releaseId)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1">
    <!-- 中列：左上角是大标题（三栏页面的标题归这一栏） -->
    <div class="flex w-72 shrink-0 flex-col border-r">
      <PaneHeader>
        <h2 class="truncate text-xl font-bold">{{ library.contextTitle }}</h2>
      </PaneHeader>
      <div class="min-h-0 flex-1 overflow-auto">
      <button
        v-for="a in visibleAlbums"
        :key="a.releaseId"
        :class="[
          'flex w-full items-center gap-2 px-3 py-2 text-left text-sm',
          detail?.releaseId === a.releaseId ? 'bg-highlight' : 'hover:bg-emphasis',
        ]"
        @click="open(a)"
      >
        <CoverArt
          :source="coverSourceOf(a)"
          :adopted-path="a.coverPath"
          :size="COVER_THUMB_SIZE"
          class="size-10 shrink-0 rounded"
          icon-class="size-4"
        />
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium">{{ a.title || '未命名专辑' }}</p>
          <p class="truncate text-xs text-muted-color">{{ display(a.credit) }}</p>
        </div>
        <span class="shrink-0 text-xs tabular-nums text-muted-color">{{ countLabel(a) }}</span>
      </button>
      <p v-if="visibleAlbums.length === 0 && !loading" class="px-3 py-4 text-sm text-muted-color">
        {{ filter ? '没有匹配的专辑' : '还没有专辑' }}
      </p>
      </div>
    </div>

    <!-- 主区：操作区在右上角，与中间栏标题同一水平线 -->
    <div class="flex min-w-0 flex-1 flex-col">
      <PaneHeader>
        <div class="flex-1" />
        <SortMenu v-model:sort-key="sortKey" v-model:desc="sortDesc" :options="sortOptions" />
        <ViewSearchBox v-model="filter" placeholder="在专辑中查找" />
      </PaneHeader>

      <div v-if="detail" class="flex shrink-0 gap-3 px-4 pb-2">
        <CoverArt
          :source="coverSourceOf(detail)"
          :adopted-path="detail.coverPath"
          :size="COVER_THUMB_SIZE"
          class="size-20 shrink-0 rounded"
          icon-class="size-6"
        />
        <div class="min-w-0 flex-1">
        <div class="flex items-baseline gap-3">
          <h3 class="text-base font-semibold">{{ detail.title }}</h3>
          <span class="text-sm text-muted-color">
            {{ detail.ownedCount }}<template v-if="detail.totalCount">/{{ detail.totalCount }}</template
            ><template v-else>/-</template> 首
          </span>
          <div class="flex-1" />
          <SecondaryButton
            variant="outlined"
            size="small"
            :class="onlyOwned ? 'bg-highlight!' : ''"
            @click="toggleOnlyOwned"
          >
            仅显示已有
          </SecondaryButton>
        </div>
        <p class="truncate text-xs text-muted-color">{{ display(detail.credit) }}</p>
        <p class="mt-0.5 text-xs text-muted-color">
          {{ display(albumTypeLabel(detail.primaryType)) }} · {{ display(detail.year) }} ·
          {{ display(detail.mediumFormat) }} · {{ display(detail.country) }}
          <template v-if="detail.discTotal"> · {{ detail.discTotal }} 碟</template>
          <template v-if="detail.releaseCount && detail.releaseCount > 1">
            · 共 {{ detail.releaseCount }} 个版本
          </template>
        </p>
        <p v-if="detail.label || detail.catalogNumber" class="mt-0.5 text-xs text-muted-color">
          {{ display(detail.label) }} · {{ display(detail.catalogNumber) }}
        </p>
        <p v-if="detail.fetchedAt" class="mt-0.5 flex items-center gap-1 text-xs text-muted-color">
          清单更新于 {{ new Date(detail.fetchedAt).toLocaleDateString() }}
          <SecondaryButton
            variant="text"
            size="small"
            :disabled="fetching"
            @click="completeTracklist(true)"
          >
            {{ fetching ? '获取中…' : '重新获取' }}
          </SecondaryButton>
        </p>
        </div>
      </div>

      <TrackTable
        v-if="detail"
        :rows="toRows()"
        fixed-order
        @open="(r) => emit('openTrack', r.id)"
        @edit="(r) => emit('editTrack', r.id)"
        @add-ghost-to-library="addGhost"
        @add-to-collection="(rows, kind) => emit('addToCollection', rows, kind)"
        @view-artist="(r) => emit('viewArtist', r)"
        @copy-info="(rows) => emit('copyInfo', rows)"
        @remove="(rows) => emit('removeTracks', rows)"
        @view-upstream="(r) => emit('viewUpstream', r)"
        @export-scope="(scope) => emit('exportScope', scope)"
        @selection-change="(ids) => emit('selectionChange', ids)"
      />

      <!-- 清单不完整时不伪装完整：末尾一行提示与补全动作 -->
      <div
        v-if="detail?.incomplete"
        class="flex shrink-0 items-center gap-2 border-t px-4 py-2 text-xs text-muted-color"
      >
        <span v-if="detail.tracklistState === 'failed'" class="text-danger-color">
          清单获取失败{{ detail.tracklistError ? `：${detail.tracklistError}` : '' }}
        </span>
        <span v-else>清单不完整</span>
        <SecondaryButton
          v-if="detail.releaseRef"
          variant="outlined"
          size="small"
          :disabled="fetching"
          @click="completeTracklist(true)"
        >
          {{ detail.tracklistState === 'failed' ? '重试' : '补全清单' }}
        </SecondaryButton>
      </div>

      <div v-if="!detail" class="flex flex-1 items-center justify-center text-sm text-muted-color">
        从左侧选择一张专辑
      </div>
      <Message v-if="error" severity="error" size="small" class="shrink-0 rounded-none">
        {{ error }}
      </Message>
    </div>
  </div>
</template>
