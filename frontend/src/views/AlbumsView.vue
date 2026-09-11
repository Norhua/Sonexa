<script setup lang="ts">
/**
 * 专辑浏览。
 *
 * 与艺术家视图不同构——它要显示用户**没有**的曲目（灰色行）。
 * 至少拥有一首才会出现在列表里：灰色行是「补全你已开始收藏的专辑」，
 * 不是上游目录浏览器。
 */
import { onMounted, ref } from 'vue'
import { App, display, type Models } from '@/api'
import type { Row } from '@/components/TrackTable.vue'
import TrackTable from '@/components/TrackTable.vue'

const albums = ref<Models.store.AlbumListItem[]>([])
const detail = ref<Models.main.AlbumDetail | null>(null)
const loading = ref(false)
const fetching = ref(false)
const error = ref('')

/** 「仅显示已有」开关的状态被记住。 */
const onlyOwned = ref(localStorage.getItem('sonexa.onlyOwned') === '1')

const emit = defineEmits<{
  openTrack: [trackId: number]
  editTrack: [trackId: number]
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
  detail.value = await App.GetAlbumDetail(a.key, a.level)

  // 首次打开且清单状态不是完整时补拉一次——不阻塞渲染：
  // 已入库曲目已经显示，灰色行随后补入。
  if (
    detail.value &&
    detail.value.level !== 'derived' &&
    detail.value.tracklistState !== 'complete' &&
    detail.value.provider &&
    detail.value.releaseRef
  ) {
    void completeTracklist(false)
  }
}

async function completeTracklist(force: boolean) {
  if (!detail.value?.provider || !detail.value?.releaseRef) return
  fetching.value = true
  try {
    await App.FetchTracklist(detail.value.provider, detail.value.releaseRef, force)
    detail.value = await App.GetAlbumDetail(detail.value.key, detail.value.level)
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
    detail.value = await App.GetAlbumDetail(detail.value.key, detail.value.level)
    await load()
  } catch (e) {
    error.value = String(e)
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1">
    <!-- 中列：专辑列表 -->
    <div class="w-72 shrink-0 overflow-auto border-r">
      <button
        v-for="a in albums"
        :key="`${a.level}-${a.key}`"
        :class="[
          'flex w-full items-center gap-2 px-3 py-2 text-left text-sm hover:bg-accent',
          detail?.key === a.key ? 'bg-accent' : '',
        ]"
        @click="open(a)"
      >
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium">{{ a.title || '未命名专辑' }}</p>
          <p class="truncate text-xs text-muted-foreground">{{ display(a.credit) }}</p>
        </div>
        <span class="shrink-0 text-xs tabular-nums text-muted-foreground">{{ countLabel(a) }}</span>
      </button>
      <p v-if="albums.length === 0 && !loading" class="px-3 py-4 text-sm text-muted-foreground">
        还没有专辑
      </p>
    </div>

    <!-- 主区：专辑详情 -->
    <div class="flex min-w-0 flex-1 flex-col">
      <div v-if="detail" class="shrink-0 border-b px-4 py-2">
        <div class="flex items-baseline gap-3">
          <h2 class="text-base font-semibold">{{ detail.title }}</h2>
          <span class="text-sm text-muted-foreground">
            {{ detail.ownedCount }}<template v-if="detail.totalCount">/{{ detail.totalCount }}</template
            ><template v-else>/-</template> 首
          </span>
          <div class="flex-1" />
          <button
            class="rounded border px-2 py-0.5 text-xs hover:bg-accent"
            :class="onlyOwned ? 'bg-accent' : ''"
            @click="toggleOnlyOwned"
          >
            仅显示已有
          </button>
        </div>
        <p class="mt-0.5 text-xs text-muted-foreground">
          {{ display(detail.primaryType) }} · {{ display(detail.year) }} ·
          {{ display(detail.mediumFormat) }} · {{ display(detail.country) }}
        </p>
        <p v-if="detail.fetchedAt" class="mt-0.5 text-xs text-muted-foreground">
          清单更新于 {{ new Date(detail.fetchedAt).toLocaleDateString() }}
          <button class="ml-1 underline" :disabled="fetching" @click="completeTracklist(true)">
            {{ fetching ? '获取中…' : '重新获取' }}
          </button>
        </p>
      </div>

      <TrackTable
        v-if="detail"
        :rows="toRows()"
        fixed-order
        @open="(r) => emit('openTrack', r.id)"
        @edit="(r) => emit('editTrack', r.id)"
        @add-ghost-to-library="addGhost"
      />

      <!-- 清单不完整时不伪装完整：末尾一行提示与补全动作 -->
      <div
        v-if="detail?.incomplete"
        class="shrink-0 border-t px-4 py-2 text-xs text-muted-foreground"
      >
        <span v-if="detail.tracklistState === 'failed'" class="text-destructive">
          清单获取失败{{ detail.tracklistError ? `：${detail.tracklistError}` : '' }}
        </span>
        <span v-else>清单不完整</span>
        <button
          v-if="detail.level !== 'derived'"
          class="ml-2 rounded border px-2 py-0.5 hover:bg-accent"
          :disabled="fetching"
          @click="completeTracklist(true)"
        >
          {{ detail.tracklistState === 'failed' ? '重试' : '补全清单' }}
        </button>
      </div>

      <div v-if="!detail" class="flex flex-1 items-center justify-center text-sm text-muted-foreground">
        从左侧选择一张专辑
      </div>
      <p v-if="error" class="shrink-0 border-t bg-destructive/10 px-4 py-2 text-xs text-destructive">
        {{ error }}
      </p>
    </div>
  </div>
</template>
