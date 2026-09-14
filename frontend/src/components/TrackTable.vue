<script setup lang="ts">
/**
 * 全应用唯一的曲目表格组件。
 *
 * 复用于：全部曲目、歌单页、标签筛选、艺术家视图、专辑视图、本地搜索结果。
 * 各处的列定义、多选行为、右键菜单结构完全一致。
 *
 * 选中交互按**文件管理器**的习惯做，而不是勾选框：整行高亮即选中状态，
 * 单击选中、Ctrl/Cmd 点击加选、Shift 点击连选、Ctrl/Cmd+A 全选、双击打开详情。
 * 勾选框列因此移除——它与整行高亮是同一件事的两种表达，留着两份只会打架。
 *
 * 灰色行（ghost）是本组件的显式变体，不是专辑视图的分叉——
 * 列是一样的，分叉会让两边逐渐漂移（加一列时只改了一边）。
 */
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { Pause, Play } from 'lucide-vue-next'
import { display, duration, type CoverSource } from '@/api'
import CoverArt from '@/components/CoverArt.vue'
import { useEnrichStore } from '@/stores/enrich'

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
  coverSource?: CoverSource | null
  coverPath?: string | null
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
    /**
     * 是否允许拖拽重排。
     *
     * 只有歌单需要显式顺序；标签忽略顺序，专辑视图按曲序读，两者都不提供。
     */
    reorderable?: boolean
    loading?: boolean
  }>(),
  { collectionId: 0, fixedOrder: false, reorderable: false, loading: false },
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
  reorder: [orderedIds: number[]]
}>()

const enrich = useEnrichStore()

const selected = ref<Set<number>>(new Set())
/** 正在被拖动的行，以及拖动经过的行——仅歌单启用。 */
const dragId = ref<number | null>(null)
const dragOverId = ref<number | null>(null)
/** Shift 连选的锚点。 */
const anchor = ref<number | null>(null)
const menu = ref<{ x: number; y: number; row: Row } | null>(null)

/**
 * 按下时的位置，用于区分「点一下」与「在文字上划选」。
 *
 * 划选必须保留：整行选中之后用户仍要能选中「标题」里的一段文字按 Cmd+C 复制。
 * 因此行选中判定放在 click 而不是 mousedown 上，并在这里比对位移——
 * 位移超过阈值且真的产生了文字选区时，这一下是划选，不动行选中状态。
 */
let pressAt: { x: number; y: number } | null = null

/** 划选判定的位移阈值（像素）。手抖几像素仍算点击。 */
const dragSlop = 4

/** 可选中的行——灰色行不可被选中。 */
const selectableRows = computed(() => props.rows.filter((r) => !r.ghost))

/** 选中计数不含灰色行。 */
const selectedCount = computed(() => selected.value.size)

/** 列表曲目数不计入灰色行。 */
const trackCount = computed(() => selectableRows.value.length)

const selectedRows = computed(() => props.rows.filter((r) => selected.value.has(r.id)))

function isSelected(row: Row) {
  return !row.ghost && selected.value.has(row.id)
}

function commit(next: Set<number>) {
  selected.value = next
  emit('selectionChange', [...next])
}

/** 换了一屏行（切视图、筛选、重载）时把已不存在的选中项丢掉。 */
watch(
  () => props.rows,
  (rows) => {
    if (selected.value.size === 0) return
    const live = new Set(rows.filter((r) => !r.ghost).map((r) => r.id))
    const kept = new Set([...selected.value].filter((id) => live.has(id)))
    if (kept.size !== selected.value.size) commit(kept)
  },
)

/** 全选跳过灰色行。 */
function selectAll() {
  if (selectableRows.value.length === 0) return
  commit(new Set(selectableRows.value.map((r) => r.id)))
}

function onRowMouseDown(row: Row, event: MouseEvent) {
  if (event.button !== 0) return
  pressAt = { x: event.clientX, y: event.clientY }
  // Shift 点击时浏览器会顺手把整段文字选进选区（读起来就是「整页变蓝底」）。
  // 行的连续选择由 click 处理，这里只需阻止那个默认行为。
  if (event.shiftKey) event.preventDefault()
}

/** 这一下是不是在划选文字。 */
function isTextDrag(event: MouseEvent): boolean {
  if (!pressAt) return false
  const moved =
    Math.abs(event.clientX - pressAt.x) + Math.abs(event.clientY - pressAt.y)
  if (moved <= dragSlop) return false
  const selection = window.getSelection()
  return !!selection && !selection.isCollapsed
}

function onRowClick(row: Row, event: MouseEvent) {
  const textDrag = isTextDrag(event)
  pressAt = null
  // 灰色行不可选中、也没有可展示的详情
  if (row.ghost) return
  // 在文字上划选：保留这段选区，不动行选中状态
  if (textDrag) return

  if (event.metaKey || event.ctrlKey) {
    // 逐项加选 / 取消
    const next = new Set(selected.value)
    if (next.has(row.id)) next.delete(row.id)
    else next.add(row.id)
    anchor.value = row.id
    commit(next)
    return
  }

  if (event.shiftKey && anchor.value !== null) {
    // 连续选择：锚点到当前行之间全选
    const ids = selectableRows.value.map((r) => r.id)
    const from = ids.indexOf(anchor.value)
    const to = ids.indexOf(row.id)
    if (from >= 0 && to >= 0) {
      const [lo, hi] = from < to ? [from, to] : [to, from]
      commit(new Set(ids.slice(lo, hi + 1)))
      return
    }
  }

  // 单击：清除其他选中，仅选中这一条
  anchor.value = row.id
  commit(new Set([row.id]))
}

/** 双击打开详情抽屉。单击只负责选中——这是文件管理器的分工。 */
function onRowDblClick(row: Row) {
  if (row.ghost) return
  emit('open', row)
}

/** Ctrl/Cmd+A 选中列表里的所有曲目，而不是把整页文字变成蓝底。 */
function onKeyDown(event: KeyboardEvent) {
  if (event.key !== 'a' && event.key !== 'A') return
  if (!event.metaKey && !event.ctrlKey) return
  if (isEditable(event.target)) return // 输入框里的全选仍归输入框
  event.preventDefault()
  selectAll()
}

function isEditable(target: EventTarget | null): boolean {
  const el = target as HTMLElement | null
  if (!el || !el.tagName) return false
  const tag = el.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || el.isContentEditable
}

/** 拖拽开始：记下被拖动的行。 */
function onDragStart(row: Row, event: DragEvent) {
  if (!props.reorderable || row.ghost) return
  dragId.value = row.id
  event.dataTransfer?.setData('text/plain', String(row.id))
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}

function onDragOver(row: Row, event: DragEvent) {
  if (!props.reorderable || dragId.value === null || row.ghost) return
  event.preventDefault()
  dragOverId.value = row.id
}

/** 放下：就地重排并把新顺序交给父组件持久化。 */
function onDrop(row: Row, event: DragEvent) {
  if (!props.reorderable || dragId.value === null || row.ghost) return
  event.preventDefault()

  const ids = props.rows.filter((r: Row) => !r.ghost).map((r: Row) => r.id)
  const from = ids.indexOf(dragId.value)
  const to = ids.indexOf(row.id)
  dragId.value = null
  dragOverId.value = null
  if (from < 0 || to < 0 || from === to) return

  const next = [...ids]
  next.splice(to, 0, ...next.splice(from, 1))
  emit('reorder', next)
}

function onDragEnd() {
  dragId.value = null
  dragOverId.value = null
}

function onContextMenu(row: Row, event: MouseEvent) {
  event.preventDefault()
  if (!row.ghost && !selected.value.has(row.id)) {
    anchor.value = row.id
    commit(new Set([row.id]))
  }
  menu.value = { x: event.clientX, y: event.clientY, row }
}

function closeMenu() {
  menu.value = null
}

// 点击表格之外（侧栏、页头）也要关掉右键菜单——
// 只在表格内部监听时，菜单会一直悬在侧栏上方
onMounted(() => {
  window.addEventListener('pointerdown', closeMenu)
  window.addEventListener('keydown', onKeyDown)
})
onUnmounted(() => {
  window.removeEventListener('pointerdown', closeMenu)
  window.removeEventListener('keydown', onKeyDown)
})

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
    <!--
      工具栏：左边是计数，右边是补全信息匹配的开关与进度。
      导出入口移到右键菜单——它作用于选中项，那正是右键的语境。
    -->
    <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs text-muted-foreground">
      <span>{{ trackCount }} 首曲目</span>
      <span v-if="selectedCount > 0">· 已选 {{ selectedCount }} 项</span>
      <div class="flex-1" />
      <div class="flex items-center gap-1.5" :title="enrich.hint">
        <span>补全信息匹配</span>
        <button
          class="flex items-center gap-1 rounded border px-1.5 py-0.5 hover:bg-accent disabled:opacity-40"
          :disabled="enrich.idleEmpty && !enrich.running"
          @click.stop="enrich.toggle()"
        >
          <component :is="enrich.running ? Pause : Play" class="size-3" />
          {{ enrich.running ? '暂停' : '开始' }}
        </button>
        <span class="tabular-nums">{{ enrich.label }}</span>
      </div>
    </div>

    <div class="min-h-0 flex-1 overflow-auto">
      <!--
        select-none 在表格级，select-text 在文字单元格级：
        这样在标题/艺术家等文字上按下拖动仍能像普通网页那样划选并复制，
        而在行的其余部分拖动不会留下一片蓝底选区。
      -->
      <table class="w-full select-none border-collapse text-sm">
        <thead class="sticky top-0 z-10 bg-background">
          <tr class="border-b text-left text-xs text-muted-foreground">
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
            :draggable="reorderable && !row.ghost"
            :class="[
              'border-b',
              dragOverId === row.id && dragId !== row.id ? 'outline outline-1 outline-primary' : '',
              dragId === row.id ? 'opacity-50' : '',
              // 灰色行：降低对比度，不加删除线——它不是「已失效」，是「你还没有」
              row.ghost
                ? 'text-ghost-foreground cursor-default'
                : 'cursor-default hover:bg-accent/40',
              // 勾选框没了，整行底色是选中状态的**唯一**信号，因此它必须明显强于
              // 悬停态：两者都用 accent 时，选中与「鼠标恰好停在这一行」分不开。
              isSelected(row) ? 'bg-primary/15' : '',
            ]"
            @mousedown="onRowMouseDown(row, $event)"
            @click="onRowClick(row, $event)"
            @dblclick="onRowDblClick(row)"
            @contextmenu="onContextMenu(row, $event)"
            @dragstart="onDragStart(row, $event)"
            @dragover="onDragOver(row, $event)"
            @drop="onDrop(row, $event)"
            @dragend="onDragEnd"
          >
            <td class="select-text px-2 py-1.5 text-xs tabular-nums">
              {{ row.trackNumber ?? '' }}
            </td>
            <td class="px-2 py-1.5">
              <!-- 取图失败（CAA 404 是常态）只影响该行：显示状态图标，其余列照常 -->
              <CoverArt
                :source="row.coverSource"
                :adopted-path="row.coverPath"
                class="size-8 rounded"
              />
            </td>
            <td class="max-w-0 select-text truncate px-2 py-1.5" :title="row.title">
              {{ row.title }}
            </td>
            <td class="max-w-0 select-text truncate px-2 py-1.5" :title="row.artist ?? ''">
              {{ display(row.artist) }}
            </td>
            <td class="max-w-0 select-text truncate px-2 py-1.5" :title="row.album ?? ''">
              {{ display(row.album) }}
            </td>
            <td class="select-text px-2 py-1.5 tabular-nums">{{ duration(row.durationMs) }}</td>
            <td class="select-text px-2 py-1.5 tabular-nums">{{ display(row.year) }}</td>
            <td class="select-text px-2 py-1.5">{{ display(row.mediumFormat) }}</td>
            <td class="select-text px-2 py-1.5">{{ display(row.country) }}</td>
            <td class="px-2 py-1.5 text-xs">
              <!-- 「其他版本」点击跳到用户已有的那一首（15.8） -->
              <button
                v-if="row.otherVersion"
                class="rounded bg-secondary px-1.5 py-0.5 text-secondary-foreground hover:bg-accent"
                title="跳到已有的那一首"
                @click.stop="emit('open', row)"
              >
                其他版本
              </button>
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
            <td colspan="10" class="px-3 py-8 text-center text-muted-foreground">
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
      @pointerdown.stop
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
        <!-- 导出：从工具栏移到这里。选中项在最前——右键的语境就是它 -->
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent disabled:opacity-40"
          :disabled="selectedCount === 0"
          @click="act(() => emit('exportScope', 'selection'))">
          导出选中项…
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('exportScope', 'filtered'))">
          导出当前列表…
        </button>
        <button class="block w-full px-3 py-1.5 text-left hover:bg-accent"
          @click="act(() => emit('exportScope', 'all'))">
          导出全库…
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
