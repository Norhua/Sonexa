<script setup lang="ts">
/**
 * 全应用唯一的曲目表格组件。
 *
 * 复用于：全部曲目、歌单页、标签筛选、艺术家视图、专辑视图、本地搜索结果。
 * 各处的列定义、多选行为、右键菜单结构完全一致。
 *
 * 灰色行（ghost）是本组件的显式变体，不是专辑视图的分叉——
 * 列是一样的，分叉会让两边逐渐漂移（加一列时只改了一边）。
 */
import { computed, ref } from 'vue'
import { display, duration, coverUrl } from '@/api'

/** 表格的一行。灰色行与普通行用同一个类型，靠 ghost 区分。 */
export interface Row {
  id: number
  title: string
  artist?: string | null
  album?: string | null
  year?: number | null
  trackNumber?: number | null
  discNumber?: number | null
  durationMs?: number | null
  genre?: string | null
  mediumFormat?: string | null
  country?: string | null
  coverKey?: string | null
  /** 灰色行：未入库的清单曲目。 */
  ghost?: boolean
  /** 「其他版本」徽章：标识未命中但录音标识命中某曲目。 */
  otherVersion?: boolean
  /** 灰色行的上游标识，供「加入曲库」直接带溯源入库。 */
  trackRef?: string | null
  recordingRef?: string | null
}

const props = withDefaults(
  defineProps<{
    rows: Row[]
    /** 当前所在歌单；非零时右键菜单出现「从当前歌单移除」。 */
    collectionId?: number
    /** 专辑视图固定按碟号与音轨号排序，不受表格默认排序影响。 */
    fixedOrder?: boolean
    loading?: boolean
  }>(),
  { collectionId: 0, fixedOrder: false, loading: false },
)

const emit = defineEmits<{
  open: [row: Row]
  edit: [row: Row]
  addToCollection: [rows: Row[], kind: 'playlist' | 'tag']
  viewArtist: [row: Row]
  copyInfo: [rows: Row[]]
  removeFromCollection: [rows: Row[]]
  remove: [rows: Row[]]
  addGhostToLibrary: [row: Row]
  viewUpstream: [row: Row]
  selectionChange: [ids: number[]]
  exportScope: [scope: 'playlist' | 'filtered' | 'selection' | 'all']
}>()

const selected = ref<Set<number>>(new Set())
const lastClicked = ref<number | null>(null)
const menu = ref<{ x: number; y: number; row: Row } | null>(null)

/** 可选中的行——灰色行不可被选中。 */
const selectableRows = computed(() => props.rows.filter((r) => !r.ghost))

/** 选中计数不含灰色行。 */
const selectedCount = computed(() => selected.value.size)

/** 列表曲目数不计入灰色行。 */
const trackCount = computed(() => selectableRows.value.length)

const allSelected = computed(
  () => selectableRows.value.length > 0 && selected.value.size === selectableRows.value.length,
)

const selectedRows = computed(() => props.rows.filter((r) => selected.value.has(r.id)))

function isSelected(row: Row) {
  return !row.ghost && selected.value.has(row.id)
}

/** 全选跳过灰色行。 */
function toggleAll() {
  if (allSelected.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(selectableRows.value.map((r) => r.id))
  }
  emit('selectionChange', [...selected.value])
}

function onRowClick(row: Row, event: MouseEvent) {
  // 灰色行不可选中、单击不打开详情抽屉——它没有可展示的详情
  if (row.ghost) return

  if (event.metaKey || event.ctrlKey) {
    // 逐项加选
    const next = new Set(selected.value)
    next.has(row.id) ? next.delete(row.id) : next.add(row.id)
    selected.value = next
    lastClicked.value = row.id
    emit('selectionChange', [...selected.value])
    return
  }

  if (event.shiftKey && lastClicked.value !== null) {
    // 连续选择
    const ids = selectableRows.value.map((r) => r.id)
    const from = ids.indexOf(lastClicked.value)
    const to = ids.indexOf(row.id)
    if (from >= 0 && to >= 0) {
      const [lo, hi] = from < to ? [from, to] : [to, from]
      selected.value = new Set(ids.slice(lo, hi + 1))
      emit('selectionChange', [...selected.value])
      return
    }
  }

  selected.value = new Set([row.id])
  lastClicked.value = row.id
  emit('selectionChange', [...selected.value])
  emit('open', row)
}

function onContextMenu(row: Row, event: MouseEvent) {
  event.preventDefault()
  if (!row.ghost && !selected.value.has(row.id)) {
    selected.value = new Set([row.id])
    emit('selectionChange', [...selected.value])
  }
  menu.value = { x: event.clientX, y: event.clientY, row }
}

function closeMenu() {
  menu.value = null
}

/** 右键菜单的动作目标：优先用选中项，否则用被右键的那一行。 */
function menuTargets(): Row[] {
  if (!menu.value) return []
  if (menu.value.row.ghost) return [menu.value.row]
  return selectedRows.value.length > 0 ? selectedRows.value : [menu.value.row]
}

function act(fn: () => void) {
  fn()
  closeMenu()
}

defineExpose({ selectedCount, trackCount, selectedRows })
</script>

<template>
  <div class="flex h-full flex-col" @click="closeMenu">
    <!-- 工具栏：导出入口，范围可选 -->
    <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs text-muted-foreground">
      <span>{{ trackCount }} 首曲目</span>
      <span v-if="selectedCount > 0">· 已选 {{ selectedCount }} 项</span>
      <div class="flex-1" />
      <div class="flex items-center gap-1">
        <span>导出</span>
        <button
          v-if="collectionId"
          class="rounded border px-2 py-0.5 hover:bg-accent"
          @click="emit('exportScope', 'playlist')"
        >
          当前歌单
        </button>
        <button
          class="rounded border px-2 py-0.5 hover:bg-accent"
          @click="emit('exportScope', 'filtered')"
        >
          当前筛选结果
        </button>
        <button
          class="rounded border px-2 py-0.5 hover:bg-accent disabled:opacity-40"
          :disabled="selectedCount === 0"
          @click="emit('exportScope', 'selection')"
        >
          选中项
        </button>
        <button
          class="rounded border px-2 py-0.5 hover:bg-accent"
          @click="emit('exportScope', 'all')"
        >
          全库
        </button>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-auto">
      <table class="w-full border-collapse text-sm">
        <thead class="sticky top-0 z-10 bg-background">
          <tr class="border-b text-left text-xs text-muted-foreground">
            <th class="w-8 px-2 py-2">
              <input
                type="checkbox"
                :checked="allSelected"
                :disabled="selectableRows.length === 0"
                @change="toggleAll"
                @click.stop
              />
            </th>
            <th class="w-10 px-2 py-2">#</th>
            <th class="w-12 px-2 py-2"></th>
            <th class="px-2 py-2">标题</th>
            <th class="px-2 py-2">艺术家</th>
            <th class="px-2 py-2">专辑</th>
            <th class="w-16 px-2 py-2">时长</th>
            <th class="w-16 px-2 py-2">年份</th>
            <th class="w-20 px-2 py-2">载体</th>
            <th class="w-16 px-2 py-2">国家</th>
            <th class="w-24 px-2 py-2">状态</th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="row in rows"
            :key="row.ghost ? `ghost-${row.discNumber}-${row.trackNumber}` : row.id"
            :class="[
              'border-b',
              // 灰色行：降低对比度，不加删除线——它不是「已失效」，是「你还没有」
              row.ghost
                ? 'text-ghost-foreground cursor-default'
                : 'cursor-pointer hover:bg-accent/50',
              isSelected(row) ? 'bg-accent' : '',
            ]"
            @click="onRowClick(row, $event)"
            @contextmenu="onContextMenu(row, $event)"
          >
            <td class="px-2 py-1.5">
              <input
                v-if="!row.ghost"
                type="checkbox"
                :checked="isSelected(row)"
                @click.stop="onRowClick(row, $event)"
              />
            </td>
            <td class="px-2 py-1.5 text-xs tabular-nums">
              {{ row.trackNumber ?? '' }}
            </td>
            <td class="px-2 py-1.5">
              <img
                v-if="row.coverKey"
                :src="coverUrl(row.coverKey, 250)"
                loading="lazy"
                class="size-8 rounded object-cover"
                alt=""
              />
              <div v-else class="size-8 rounded bg-muted" />
            </td>
            <td class="max-w-0 truncate px-2 py-1.5" :title="row.title">{{ row.title }}</td>
            <td class="max-w-0 truncate px-2 py-1.5" :title="row.artist ?? ''">
              {{ display(row.artist) }}
            </td>
            <td class="max-w-0 truncate px-2 py-1.5" :title="row.album ?? ''">
              {{ display(row.album) }}
            </td>
            <td class="px-2 py-1.5 tabular-nums">{{ duration(row.durationMs) }}</td>
            <td class="px-2 py-1.5 tabular-nums">{{ display(row.year) }}</td>
            <td class="px-2 py-1.5">{{ display(row.mediumFormat) }}</td>
            <td class="px-2 py-1.5">{{ display(row.country) }}</td>
            <td class="px-2 py-1.5 text-xs">
              <span
                v-if="row.otherVersion"
                class="rounded bg-secondary px-1.5 py-0.5 text-secondary-foreground"
              >
                其他版本
              </span>
              <button
                v-else-if="row.ghost"
                class="rounded border px-1.5 py-0.5 hover:bg-accent"
                @click.stop="emit('addGhostToLibrary', row)"
              >
                加入曲库
              </button>
            </td>
          </tr>

          <tr v-if="rows.length === 0 && !loading">
            <td colspan="11" class="px-3 py-8 text-center text-muted-foreground">
              <slot name="empty">没有曲目</slot>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <!-- 右键菜单 -->
    <div
      v-if="menu"
      class="fixed z-50 min-w-44 rounded-md border bg-popover py-1 text-sm shadow-md"
      :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
      @click.stop
    >
      <!-- 灰色行的菜单只有两项：它不是曲目，没有编辑/删除/加入歌单等动作 -->
      <template v-if="menu.row.ghost">
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('addGhostToLibrary', menu!.row))">
          加入曲库
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('viewUpstream', menu!.row))">
          查看上游
        </button>
      </template>
      <template v-else>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('edit', menu!.row))">
          编辑
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('addToCollection', menuTargets(), 'playlist'))">
          添加到歌单…
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('addToCollection', menuTargets(), 'tag'))">
          添加标签…
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('viewArtist', menu!.row))">
          在艺术家视图中查看…
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('copyInfo', menuTargets()))">
          复制信息
        </button>
        <button v-if="collectionId" class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('removeFromCollection', menuTargets()))">
          从当前歌单移除
        </button>
        <div class="my-1 border-t" />
        <button class="block w-full px-3 py-1.5 text-left text-destructive hover:bg-accent"
          @click="act(() => emit('remove', menuTargets()))">
          删除
        </button>
      </template>
    </div>
  </div>
</template>
