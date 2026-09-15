<script setup lang="ts">
/**
 * 全应用唯一的曲目表格组件。
 *
 * 复用于：全部曲目、歌单页、标签筛选、艺术家视图、专辑视图、本地搜索结果。
 * 各处的列定义、多选行为、右键菜单结构完全一致。
 *
 * 表格本体是 Volt 的 DataTable（`@/volt/DataTable.vue`）：多选、连选、
 * 键盘导航、排序、粘性表头这些都由它提供，本组件只补三件它没有的事——
 * 灰色行不可选中、文字划选不改变行选中、右键菜单按目标行变形。
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
import Column from 'primevue/column'
import type { MenuItem } from 'primevue/menuitem'
import DataTable from '@/volt/DataTable.vue'
import ContextMenu from '@/volt/ContextMenu.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
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
  /** 已采用封面的落盘路径，优先于临时缩略图。 */
  adoptedCoverPath?: string | null
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
    /** 专辑视图固定按碟号与音轨号排序，不受表格排序影响。 */
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

/** 排序维度：DataTable 就地排序，'default' 由使用方置空 sortField 表达。 */
const sortField = defineModel<string | null>('sortField', { default: null })
const sortOrder = defineModel<number>('sortOrder', { default: 1 })

const enrich = useEnrichStore()

const selection = ref<Row[]>([])
const table = ref<HTMLElement | null>(null)
const menu = ref<InstanceType<typeof ContextMenu> | null>(null)
/** 右键命中的那一行。菜单的形态与动作目标都由它决定。 */
const menuRow = ref<Row | null>(null)

/**
 * 按下时的位置，用于区分「点一下」与「在文字上划选」。
 *
 * 划选必须保留：整行选中之后用户仍要能选中「标题」里的一段文字按 Cmd+C 复制。
 * DataTable 在 click 上判定行选中，因此这里在**捕获阶段**拦一道——
 * 位移超过阈值且真的产生了文字选区时，这一下是划选，不让它到达行。
 */
let pressAt: { x: number; y: number } | null = null

/** 划选判定的位移阈值（像素）。手抖几像素仍算点击。 */
const dragSlop = 4

/** 可选中的行——灰色行不可被选中。 */
const selectableRows = computed(() => props.rows.filter((r) => !r.ghost))

/** 选中计数不含灰色行。 */
const selectedCount = computed(() => selection.value.length)

/** 列表曲目数不计入灰色行。 */
const trackCount = computed(() => selectableRows.value.length)

/**
 * 选中集合的唯一入口。
 *
 * DataTable 有好几条路径会写选中集合（单击、Shift 连选、Cmd+A、键盘上下），
 * 而「灰色行不可选中」是对**集合**的约束，不是对某条路径的约束——
 * 在这里过滤一次，每条路径就都守住了。
 */
function onSelectionUpdate(next: Row[] | null) {
  const incoming = next ?? []
  const cleaned = incoming.filter((r) => !r.ghost)
  // 点到灰色行：它不可选中，但也不该顺手把已有的选中清掉
  if (cleaned.length === 0 && incoming.length > 0) return
  commit(cleaned)
}

function commit(next: Row[]) {
  selection.value = next
  emit('selectionChange', next.map((r) => r.id))
}

/** 换了一屏行（切视图、筛选、重载）时把已不存在的选中项丢掉。 */
watch(
  () => props.rows,
  (rows) => {
    if (selection.value.length === 0) return
    const live = new Set(rows.filter((r) => !r.ghost).map((r) => r.id))
    const kept = selection.value.filter((r) => live.has(r.id))
    if (kept.length !== selection.value.length) commit(kept)
  },
)

/** 全选跳过灰色行。 */
function selectAll() {
  if (selectableRows.value.length === 0) return
  commit([...selectableRows.value])
}

/** 这一下是不是在划选文字。 */
function isTextDrag(event: MouseEvent): boolean {
  if (!pressAt) return false
  const moved =
    Math.abs(event.clientX - pressAt.x) + Math.abs(event.clientY - pressAt.y)
  if (moved <= dragSlop) return false
  const selected = window.getSelection()
  return !!selected && !selected.isCollapsed
}

function onMouseDownCapture(event: MouseEvent) {
  if (event.button !== 0) return
  pressAt = { x: event.clientX, y: event.clientY }
  // Shift 点击时浏览器会顺手把整段文字选进选区（读起来就是「整页变蓝底」）。
  // 行的连续选择由 click 处理，这里只需阻止那个默认行为。
  if (event.shiftKey) event.preventDefault()
}

/**
 * 捕获阶段拦下「划选文字」的那一次 click。
 *
 * 监听挂在表格外层且用捕获阶段，因此在 DataTable 绑在 <tr> 上的
 * click 处理器之前执行，stopPropagation 后那一下就到不了行，
 * 行选中状态不动、选区留在原地。
 */
function onClickCapture(event: MouseEvent) {
  const textDrag = isTextDrag(event)
  pressAt = null
  if (textDrag) event.stopPropagation()
}

/**
 * 冒泡阶段把 draggable 设回来。
 *
 * DataTable 在它自己的行 mousedown 里一律 `draggable = false`——它只认自带的
 * 拖拽手柄列，而我们不加那一列（列定义在各视图之间必须一致）。这个监听在
 * 外层的冒泡阶段，于是在它之后执行，把歌单行的可拖拽状态设回来。
 *
 * 在文字单元格上按下时不设：那一下用户是要划选文字，不是要搬动行。
 */
function onMouseDownBubble(event: MouseEvent) {
  const tr = (event.target as HTMLElement | null)?.closest?.('tr[data-p-index]')
  if (!(tr instanceof HTMLTableRowElement)) return
  const onText = !!(event.target as HTMLElement).closest('[data-text-cell]')
  tr.draggable = props.reorderable && !onText
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

/** 双击打开详情抽屉。单击只负责选中——这是文件管理器的分工。 */
function onRowDblClick(event: { data: Row }) {
  if (event.data.ghost) return
  emit('open', event.data)
}

/** 右键：不在选中集合里的行先被选中，再弹菜单。 */
function onRowContextMenu(event: { originalEvent: Event; data: Row }) {
  const row = event.data
  menuRow.value = row
  if (!row.ghost && !selection.value.some((r) => r.id === row.id)) {
    commit([row])
  }
  menu.value?.show(event.originalEvent)
}

/** 右键菜单的动作目标：优先用选中项，否则用被右键的那一行。 */
function menuTargets(): Row[] {
  const row = menuRow.value
  if (!row) return []
  if (row.ghost) return [row]
  return selection.value.length > 0 ? [...selection.value] : [row]
}

/**
 * 菜单模型随命中的行变形。
 *
 * 灰色行只有两项：它不是曲目，没有编辑/删除/加入歌单等动作。
 */
const menuModel = computed<MenuItem[]>(() => {
  const row = menuRow.value
  if (!row) return []
  if (row.ghost) {
    return [
      { label: '加入曲库', command: () => emit('addGhostToLibrary', row) },
      { label: '查看上游', command: () => emit('viewUpstream', row) },
    ]
  }
  const items: MenuItem[] = [
    { label: '编辑', command: () => emit('edit', row) },
    { label: '添加到歌单…', command: () => emit('addToCollection', menuTargets(), 'playlist') },
    { label: '添加标签…', command: () => emit('addToCollection', menuTargets(), 'tag') },
    { label: '在艺术家视图中查看…', command: () => emit('viewArtist', row) },
    { label: '复制信息', command: () => emit('copyInfo', menuTargets()) },
  ]
  if (props.collectionId) {
    items.push({
      label: '从当前歌单移除',
      command: () => emit('removeFromCollection', menuTargets()),
    })
  }
  // 导出：从工具栏移到这里。选中项在最前——右键的语境就是它
  items.push(
    { separator: true },
    {
      label: '导出选中项…',
      disabled: selectedCount.value === 0,
      command: () => emit('exportScope', 'selection'),
    },
    { label: '导出当前列表…', command: () => emit('exportScope', 'filtered') },
    { label: '导出全库…', command: () => emit('exportScope', 'all') },
    { separator: true },
    { label: '删除', class: 'text-danger-color', command: () => emit('remove', menuTargets()) },
  )
  return items
})

/** 灰色行：降低对比度，不加删除线——它不是「已失效」，是「你还没有」。 */
function rowClass(row: Row) {
  return row.ghost ? 'text-ghost-color cursor-default' : ''
}

// ── 拖拽重排。歌单的人工顺序只在默认顺序下可拖，排序视图里落点没有意义 ──
const dragId = ref<number | null>(null)

function rowAt(event: Event): Row | null {
  const tr = (event.target as HTMLElement | null)?.closest?.('tr[data-p-index]')
  if (!tr) return null
  const index = Number(tr.getAttribute('data-p-index'))
  return props.rows[index] ?? null
}

function onDragStart(event: DragEvent) {
  const row = rowAt(event)
  if (!props.reorderable || !row || row.ghost) {
    event.preventDefault()
    return
  }
  dragId.value = row.id
  event.dataTransfer?.setData('text/plain', String(row.id))
  if (event.dataTransfer) event.dataTransfer.effectAllowed = 'move'
}

function onDragOver(event: DragEvent) {
  if (!props.reorderable || dragId.value === null) return
  const row = rowAt(event)
  if (!row || row.ghost) return
  event.preventDefault()
}

/** 放下：就地重排并把新顺序交给父组件持久化。 */
function onDrop(event: DragEvent) {
  const row = rowAt(event)
  if (!props.reorderable || dragId.value === null || !row || row.ghost) return
  event.preventDefault()

  const ids = props.rows.filter((r) => !r.ghost).map((r) => r.id)
  const from = ids.indexOf(dragId.value)
  const to = ids.indexOf(row.id)
  dragId.value = null
  if (from < 0 || to < 0 || from === to) return

  const next = [...ids]
  next.splice(to, 0, ...next.splice(from, 1))
  emit('reorder', next)
}

function onDragEnd() {
  dragId.value = null
}

onMounted(() => window.addEventListener('keydown', onKeyDown))
onUnmounted(() => window.removeEventListener('keydown', onKeyDown))

defineExpose({ selectedCount, trackCount })
</script>

<template>
  <div class="flex h-full min-h-0 flex-col">
    <!--
      工具栏：左边是计数，右边是补全信息匹配的开关与进度。
      导出入口移到右键菜单——它作用于选中项，那正是右键的语境。
    -->
    <div class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs text-muted-color">
      <span>{{ trackCount }} 首曲目</span>
      <span v-if="selectedCount > 0">· 已选 {{ selectedCount }} 项</span>
      <div class="flex-1" />
      <div class="flex items-center gap-1.5" :title="enrich.hint">
        <span>补全信息匹配</span>
        <SecondaryButton
          size="small"
          :disabled="enrich.idleEmpty && !enrich.running"
          @click.stop="enrich.toggle()"
        >
          <component :is="enrich.running ? Pause : Play" class="size-3" />
          {{ enrich.running ? '暂停' : '开始' }}
        </SecondaryButton>
        <span class="tabular-nums">{{ enrich.label }}</span>
      </div>
    </div>

    <!--
      select-none 在表格级、select-text 在文字单元格级：
      这样在标题/艺术家等文字上按下拖动仍能像普通网页那样划选并复制，
      而在行的其余部分拖动不会留下一片蓝底选区。
    -->
    <div
      ref="table"
      class="min-h-0 flex-1 select-none"
      @mousedown.capture="onMouseDownCapture"
      @mousedown="onMouseDownBubble"
      @click.capture="onClickCapture"
      @dragstart="onDragStart"
      @dragover="onDragOver"
      @drop="onDrop"
      @dragend="onDragEnd"
    >
      <DataTable
        :value="rows"
        data-key="id"
        :selection="selection"
        selection-mode="multiple"
        :meta-key-selection="true"
        context-menu
        :row-class="rowClass"
        :loading="loading"
        :sort-field="fixedOrder ? undefined : (sortField ?? undefined)"
        :sort-order="fixedOrder ? undefined : sortOrder"
        scrollable
        scroll-height="flex"
        row-hover
        class="h-full"
        @update:selection="onSelectionUpdate"
        @update:sort-field="(f: unknown) => (sortField = (f as string | null) ?? null)"
        @update:sort-order="(o: number | undefined) => (sortOrder = o ?? 1)"
        @row-dblclick="onRowDblClick"
        @row-contextmenu="onRowContextMenu"
      >
        <template #empty>
          <div class="px-3 py-8 text-center text-muted-color">
            <slot name="empty">没有曲目</slot>
          </div>
        </template>

        <Column field="trackNumber" header="#" class="w-10">
          <template #body="{ data }">
            <span class="text-xs tabular-nums">{{ data.trackNumber ?? '' }}</span>
          </template>
        </Column>

        <Column class="w-12">
          <template #body="{ data }">
            <!-- 取图失败（CAA 404 是常态）只影响该行：显示状态图标，其余列照常 -->
            <CoverArt
              :source="data.coverSource"
              :adopted-path="data.adoptedCoverPath"
              class="size-8 rounded"
            />
          </template>
        </Column>

        <Column field="title" header="标题" :sortable="!fixedOrder" class="max-w-0">
          <template #body="{ data }">
            <div data-text-cell class="select-text truncate" :title="data.title">
              {{ data.title }}
            </div>
          </template>
        </Column>

        <Column field="artist" header="艺术家" :sortable="!fixedOrder" class="max-w-0">
          <template #body="{ data }">
            <div data-text-cell class="select-text truncate" :title="data.artist ?? ''">
              {{ display(data.artist) }}
            </div>
          </template>
        </Column>

        <Column field="album" header="专辑" :sortable="!fixedOrder" class="max-w-0">
          <template #body="{ data }">
            <div data-text-cell class="select-text truncate" :title="data.album ?? ''">
              {{ display(data.album) }}
            </div>
          </template>
        </Column>

        <Column field="durationMs" header="时长" :sortable="!fixedOrder" class="w-16">
          <template #body="{ data }">
            <span data-text-cell class="select-text tabular-nums">
              {{ duration(data.durationMs) }}
            </span>
          </template>
        </Column>

        <Column field="year" header="年份" :sortable="!fixedOrder" class="w-16">
          <template #body="{ data }">
            <span data-text-cell class="select-text tabular-nums">{{ display(data.year) }}</span>
          </template>
        </Column>

        <Column field="mediumFormat" header="载体" class="w-20">
          <template #body="{ data }">
            <span data-text-cell class="select-text">{{ display(data.mediumFormat) }}</span>
          </template>
        </Column>

        <Column field="country" header="国家" class="w-16">
          <template #body="{ data }">
            <span data-text-cell class="select-text">{{ display(data.country) }}</span>
          </template>
        </Column>

        <Column header="状态" class="w-24">
          <template #body="{ data }">
            <!-- 「其他版本」点击跳到用户已有的那一首 -->
            <SecondaryButton
              v-if="data.otherVersion"
              size="small"
              title="跳到已有的那一首"
              @click.stop="emit('open', data)"
            >
              其他版本
            </SecondaryButton>
            <SecondaryButton
              v-else-if="data.ghost"
              size="small"
              variant="outlined"
              @click.stop="emit('addGhostToLibrary', data)"
            >
              加入曲库
            </SecondaryButton>
          </template>
        </Column>
      </DataTable>
    </div>

    <ContextMenu ref="menu" :model="menuModel" />
  </div>
</template>
