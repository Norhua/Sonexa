<script setup lang="ts">
/** 唯一曲目表格：Volt 提供结构，useListSelection 独占选择与焦点。 */
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Check, CircleCheck, Pause, Play } from 'lucide-vue-next'
import Column from 'primevue/column'
import type { DataTableSortEvent } from 'primevue/datatable'
import type { MenuItem } from 'primevue/menuitem'
import DataTable from '@/volt/DataTable.vue'
import SortIcon from '@/volt/SortIcon.vue'
import ContextMenu from '@/volt/ContextMenu.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import CoverArt from '@/components/CoverArt.vue'
import { addedTime, display, duration } from '@/api'
import { columnsFor, useTrackColumnsStore, type TrackColumnContext, type TrackColumnKey } from '@/stores/trackColumns'
import { useEnrichStore } from '@/stores/enrich'
import { listRows, sortRows, uniqueTracks, type ListRow, type Row } from '@/lib/trackRows'
import { useListSelection } from '@/lib/useListSelection'
export type { Row } from '@/lib/trackRows'

const props = withDefaults(defineProps<{
  drawerOpen?: boolean
  showEnrichment?: boolean
  rows: Row[]
  collectionId?: number
  fixedOrder?: boolean
  columnContext?: TrackColumnContext
  loading?: boolean
  /** 相同组件切换歌单/艺术家/查询时必须重置交互上下文。 */
  contextKey?: string
}>(), { collectionId: 0, fixedOrder: false, columnContext: 'library', loading: false, contextKey: '' })
const sortKey = defineModel<string>('sortKey', { default: 'default' })
const sortDesc = defineModel<boolean>('sortDesc', { default: false })
const emit = defineEmits<{
  open: [row: Row]
  edit: [row: Row]
  addToLibrary: [rows: Row[]]
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
const columns = useTrackColumnsStore()
const hasColumn = (key: TrackColumnKey) => columns.has(key, props.columnContext)
const headerMenu = ref<InstanceType<typeof ContextMenu> | null>(null)
const headerMenuModel = computed<MenuItem[]>(() => columnsFor(props.columnContext).map(column => ({
  label: column.label,
  checked: hasColumn(column.key),
  disabled: hasColumn(column.key) && columns.visibleFor(props.columnContext).length === 1,
  command: () => columns.toggle(column.key, props.columnContext),
})))
const headerMenuPT = {
  item: ({ context }: { context: { item: MenuItem } }) => ({
    role: 'menuitemcheckbox', 'aria-checked': context.item.checked,
  }),
}
const textColumns = computed(() => [
  { field: 'title', label: '标题' }, { field: 'artist', label: '艺术家' }, { field: 'album', label: '专辑' },
].filter(column => hasColumn(column.field as 'title' | 'artist' | 'album')))
const enrich = useEnrichStore()
const root = ref<HTMLElement | null>(null)
const table = ref<InstanceType<typeof DataTable> | null>(null)
const virtualized = computed(() => props.columnContext === 'syncedPlaylist' && props.rows.length > 200)
const virtualOptions = computed(() => virtualized.value ? { itemSize: 45, numToleratedItems: 10, onScrollIndexChange: restoreVirtualFocus } : undefined)
let pendingFocus: string | null = null
async function restoreVirtualFocus() {
  await nextTick()
  if (!pendingFocus || selection.focused.value !== pendingFocus) return
  const element = renderedFocusRow()
  if (element) { element.focus({ preventScroll: true }); pendingFocus = null }
}
const menu = ref<InstanceType<typeof ContextMenu> | null>(null)
const menuRow = ref<ListRow | null>(null)
const orderedRows = computed(() => sortRows(listRows(props.rows), props.fixedOrder ? 'default' : sortKey.value, sortDesc.value))
const selection = useListSelection(orderedRows)
const selectedRows = computed(() => uniqueTracks(selection.selectedRows.value))
const selectedCount = computed(() => selectedRows.value.length)
const trackCount = computed(() => props.columnContext === 'syncedPlaylist' ? props.rows.length : uniqueTracks(props.rows).length)
const sortField = computed(() => props.fixedOrder || sortKey.value === 'default' ? undefined : sortKey.value === 'duration' ? 'durationMs' : sortKey.value)
const tablePT = computed(() => ({
  virtualScroller: { root: { class: 'relative min-h-0 flex-1 overflow-auto [contain:strict] [transform:translateZ(0)]' } },
  virtualScrollerSpacer: { class: 'flex pointer-events-none' },
  table: { 'aria-label': '曲目列表', 'aria-multiselectable': 'true' },
  bodyRow: ({ context }: { context: { index: number } }) => {
    const row = orderedRows.value[context.index]
    return row ? {
      'data-row-key': row.key,
      'data-upstream-missing': !!row.upstreamMissing,
      'data-ghost': !!row.ghost,
      'aria-selected': !row.ghost && selection.selected.value.has(row.key),
      tabindex: !row.ghost && selection.focused.value === row.key ? 0 : -1,
      class: 'track-row',
    } : {}
  },
}))
watch(selectedRows, rows => emit('selectionChange', rows.map(row => row.id)), { immediate: true })
watch(() => props.contextKey, () => {
  pendingFocus = null
  selection.clear()
  selection.focused.value = orderedRows.value.find(row => !row.ghost)?.key ?? null
  if (virtualized.value) void nextTick(() => table.value?.scrollToIndex(0))
  menu.value?.hide()
  headerMenu.value?.hide()
})
watch(() => props.rows, () => { menu.value?.hide(); menuRow.value = null })
onBeforeUnmount(() => emit('selectionChange', []))

function onSort(event: DataTableSortEvent) {
  if (props.fixedOrder) return
  sortKey.value = !event.sortOrder ? 'default' : event.sortField === 'durationMs' ? 'duration' : String(event.sortField ?? 'default')
  sortDesc.value = event.sortOrder === -1
}
function rowElement(target: EventTarget | null) {
  return target instanceof Element ? target.closest<HTMLElement>('[data-row-key]') : null
}
function rowAt(target: EventTarget | null) {
  const key = rowElement(target)?.dataset.rowKey
  return orderedRows.value.find(row => row.key === key)
}
function interactive(target: EventTarget | null) {
  return target instanceof Element && !!target.closest('button, a, input, textarea, select, [contenteditable="true"], [role="combobox"]')
}
function renderedFocusRow() {
  return [...(root.value?.querySelectorAll<HTMLElement>('[data-row-key]') ?? [])]
    .find(el => el.dataset.rowKey === selection.focused.value)
}
async function focusRow() {
  await nextTick()
  const element = renderedFocusRow()
  if (!element && virtualized.value) {
    pendingFocus = selection.focused.value
    const index = orderedRows.value.findIndex(row => row.key === pendingFocus)
    if (index >= 0) table.value?.scrollToIndex(index)
    // Native scroll events may arrive after Vue's nextTick, especially WebKit.
    // The range-change callback restores focus after the target is mounted.
    void restoreVirtualFocus()
    return
  }
  pendingFocus = null
  element?.focus({ preventScroll: true })
  element?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
}
let press: { x: number; y: number } | null = null
function onMouseDown(event: MouseEvent) {
  press = null
  if (event.button !== 0 || interactive(event.target) || !rowAt(event.target)) return
  press = { x: event.clientX, y: event.clientY }
  if (event.shiftKey) event.preventDefault()
}
function onClick(event: MouseEvent) {
  if (interactive(event.target)) return
  const row = rowAt(event.target)
  const text = window.getSelection()
  const draggedText = press && Math.hypot(event.clientX - press.x, event.clientY - press.y) > 4 && text && !text.isCollapsed
  press = null
  if (draggedText) return
  if (row) {
    if (row.ghost) return
    selection.choose(row.key, event)
    if (props.drawerOpen && event.detail <= 1 && !event.ctrlKey && !event.metaKey && !event.shiftKey && !event.altKey) {
      emit('open', row)
    }
    // 普通点击后行接管焦点；划选时绝不 focus，否则 WebKit 会丢选区。
    void focusRow()
  } else if (event.target instanceof Element && !event.target.closest('thead, [data-track-toolbar]')) {
    selection.clear()
    root.value?.focus()
  }
}
function onDoubleClick(event: MouseEvent) {
  if (interactive(event.target)) return
  const row = rowAt(event.target)
  if (row && !row.ghost) emit('open', row)
}
function showMenu(row: ListRow, event: MouseEvent) {
  event.preventDefault()
  headerMenu.value?.hide()
  selection.context(row.key)
  menuRow.value = row
  void nextTick(() => {
    // 右键菜单关闭后返回当前行，而不是右键前的侧栏/旧行。
    if (!row.ghost) rowElement(event.target)?.focus({ preventScroll: true })
    menu.value?.show(event)
  })
}
function onContextMenu(event: MouseEvent) {
  if (event.target instanceof Element && event.target.closest('thead')) {
    event.preventDefault()
    menu.value?.hide()
    headerMenu.value?.show(event)
    return
  }
  if (interactive(event.target)) return
  const row = rowAt(event.target)
  if (row) showMenu(row, event)
}
function onKeyDown(event: KeyboardEvent) {
  if ((event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) && event.target instanceof Element) {
    const header = event.target.closest('thead th')
    if (header) {
      event.preventDefault()
      const box = header.getBoundingClientRect()
      menu.value?.hide()
      headerMenu.value?.show(new MouseEvent('contextmenu', { clientX: box.left, clientY: box.bottom }))
      return
    }
  }
  // 仅接管行/列表本身；输入框、工具栏、弹窗和 Teleport 菜单均不属于这里。
  if (event.isComposing || event.altKey || (event.target !== root.value && event.target !== rowElement(event.target))) return
  const meta = event.ctrlKey || event.metaKey
  const directions = { ArrowUp: 'up', ArrowDown: 'down', Home: 'first', End: 'last' } as const
  if (event.key in directions) {
    event.preventDefault()
    selection.move(directions[event.key as keyof typeof directions], event)
    void focusRow()
  } else if (meta && event.key.toLowerCase() === 'a') {
    event.preventDefault(); selection.all()
  } else if (event.key === 'Escape') {
    event.preventDefault(); selection.clear()
  } else if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    const row = orderedRows.value.find(row => row.key === selection.focused.value)
    if (!row || row.ghost) return
    if (event.key === 'Enter') emit('open', row)
    else selection.choose(row.key, { ctrlKey: true, shiftKey: event.shiftKey })
  } else if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
    const row = rowAt(event.target)
    const box = rowElement(event.target)?.getBoundingClientRect()
    if (!row || !box) return
    event.preventDefault()
    showMenu(row, new MouseEvent('contextmenu', { clientX: box.left + 32, clientY: box.bottom }))
  } else if (meta && event.key.toLowerCase() === 'c' && window.getSelection()?.isCollapsed) {
    event.preventDefault()
    if (selectedCount.value) emit('copyInfo', selectedRows.value)
  }
}
const menuModel = computed<MenuItem[]>(() => {
  const row = menuRow.value
  if (!row) return []
  if (row.ghost) return [
    { label: '加入曲库', command: () => emit('addGhostToLibrary', row) },
    { label: '查看上游', command: () => emit('viewUpstream', row) },
  ]
  if (props.columnContext === 'syncedPlaylist') return [
    { label: '打开详情', command: () => emit('open', row) },
    { label: '编辑', command: () => emit('edit', row) },
    { label: '复制信息', command: () => emit('copyInfo', selectedRows.value) },
    { label: '添加到我的音乐库', command: () => emit('addToLibrary', selectedRows.value) },
  ]
  return [
    { label: '打开详情', command: () => emit('open', row) },
    { label: '编辑', command: () => emit('edit', row) },
    { separator: true },
    { label: '添加到歌单…', command: () => emit('addToCollection', selectedRows.value, 'playlist') },
    { label: '添加标签…', command: () => emit('addToCollection', selectedRows.value, 'tag') },
    { label: '在艺术家视图中查看…', command: () => emit('viewArtist', row) },
    { label: '复制信息', command: () => emit('copyInfo', selectedRows.value) },
    ...(props.collectionId ? [{ label: '从当前歌单移除', command: () => emit('removeFromCollection', selectedRows.value) }] : []),
    { separator: true },
    { label: '导出选中项…', disabled: !selectedCount.value, command: () => emit('exportScope', 'selection') },
    { label: '导出当前列表…', command: () => emit('exportScope', 'filtered') },
    { label: '导出全库…', command: () => emit('exportScope', 'all') },
    { separator: true },
    { label: '删除', class: 'text-danger-color', command: () => emit('remove', selectedRows.value) },
  ]
})
defineExpose({ selectedCount, trackCount, selectedRows })
</script>

<template>
  <div ref="root" class="track-list flex h-full min-h-0 flex-1 flex-col outline-none" tabindex="-1" :aria-busy="loading"
    @mousedown.capture="onMouseDown" @click="onClick" @dblclick="onDoubleClick"
    @contextmenu="onContextMenu" @keydown="onKeyDown">
    <div data-track-toolbar class="flex shrink-0 items-center gap-2 border-b px-3 py-2 text-xs text-muted-color">
      <span role="status"><template v-if="loading">正在加载曲目…</template><template v-else>{{ trackCount }} {{ columnContext === 'syncedPlaylist' ? '个已导入项' : '首曲目' }}<span v-if="selectedCount"> · 已选 {{ selectedCount }} 项</span></template></span>
      <div class="flex-1" />
      <div v-if="showEnrichment && !enrich.idleEmpty" class="flex items-center gap-1.5" :title="enrich.hint" data-testid="enrichment-progress">
        <span>补全信息匹配</span>
        <SecondaryButton size="small" variant="text" pt:root:class="h-6 gap-1 p-small:px-1.5 p-small:py-0 p-small:text-xs" :disabled="enrich.idleEmpty && !enrich.running" @click="enrich.toggle()">
          <component :is="enrich.running ? Pause : Play" class="size-3" />{{ enrich.running ? '暂停' : '开始' }}
        </SecondaryButton>
        <span class="tabular-nums">{{ enrich.label }}</span>
      </div>
    </div>
    <!-- lazy only disables internal sorting/filtering. Virtualization bounds DOM,
         never the full orderedRows used by selection and batch actions. -->
    <DataTable ref="table" class="min-h-0 flex-1" :class="{ 'virtual-tracks': virtualized }" :value="orderedRows" :virtual-scroller-options="virtualOptions" data-key="key" lazy scrollable scroll-height="flex" removable-sort
      :selection="selection.selectedRows.value" :pt="tablePT" :loading="loading"
      :sort-field="sortField" :sort-order="sortDesc ? -1 : 1"
      :table-style="{ minWidth: `${columns.widthFor(columnContext)}px`, tableLayout: 'fixed' }" @sort="onSort">
      <Column v-if="hasColumn('inLibrary')" header="" style="width: 48px"><template #body="{ data }"><span :aria-label="data.inLibrary ? '已入库' : '未入库'" :title="data.inLibrary ? '已入库' : '未入库'"><CircleCheck v-if="data.inLibrary" class="size-4 text-green-600" aria-hidden="true" /></span></template></Column>
      <Column v-if="hasColumn('trackNumber')" header="音轨号" style="width: 72px"><template #body="{ data }"><span class="select-text tabular-nums">{{ display(data.trackNumber) }}</span></template></Column>
      <Column v-if="hasColumn('cover')" header="封面" style="width: 48px"><template #body="{ data }"><CoverArt :source="data.coverSource" usage="thumbnail" class="size-8 rounded" /></template></Column>
      <Column v-for="column in textColumns"
        :key="column.field" :field="column.field" :header="column.label" :sortable="!fixedOrder">
        <template #sorticon="slotProps"><SortIcon v-bind="slotProps" /></template>
        <template #body="{ data }"><span data-text-cell class="block select-text truncate" :title="data[column.field] ?? ''">{{ display(data[column.field]) }}</span></template>
      </Column>
      <Column v-if="hasColumn('duration')" field="durationMs" header="时长" :sortable="!fixedOrder" style="width: 76px"><template #sorticon="slotProps"><SortIcon v-bind="slotProps" /></template><template #body="{ data }"><span class="select-text tabular-nums">{{ duration(data.durationMs) }}</span></template></Column>
      <Column v-if="hasColumn('year')" field="year" header="年份" :sortable="!fixedOrder" style="width: 76px"><template #sorticon="slotProps"><SortIcon v-bind="slotProps" /></template><template #body="{ data }"><span class="select-text tabular-nums">{{ display(data.year) }}</span></template></Column>
      <Column v-if="hasColumn('medium')" header="载体" style="width: 80px"><template #body="{ data }"><span class="block select-text truncate" :title="data.mediumFormat">{{ display(data.mediumFormat) }}</span></template></Column>
      <Column v-if="hasColumn('country')" header="国家" style="width: 64px"><template #body="{ data }"><span class="select-text">{{ display(data.country) }}</span></template></Column>
      <Column v-if="hasColumn('addedAt')" field="addedAt" header="加入时间" :sortable="!fixedOrder" style="width: 112px"><template #sorticon="slotProps"><SortIcon v-bind="slotProps" /></template><template #body="{ data }"><span class="select-text tabular-nums">{{ addedTime(data.addedAt) }}</span></template></Column>
      <template #empty><div class="py-10 text-center text-muted-color"><slot name="empty">{{ loading ? '正在加载曲目…' : '没有曲目' }}</slot></div></template>
    </DataTable>
    <ContextMenu ref="menu" :model="menuModel" />
    <ContextMenu ref="headerMenu" :model="headerMenuModel" :pt="headerMenuPT" aria-label="显示列">
      <template #item="{ item, props: itemProps }">
        <a v-bind="itemProps.action">
          <span class="size-4 shrink-0"><Check v-if="item.checked" class="size-4" aria-hidden="true" /></span>
          <span>{{ item.label }}</span>
        </a>
      </template>
    </ContextMenu>
  </div>
</template>

<style scoped>
.track-list :deep(.virtual-tracks .track-row) { height: 45px; }
.track-list :deep(.virtual-tracks td) { height: 45px; overflow: hidden; white-space: nowrap; }
.track-list :deep(table) { user-select: none; }
.track-list :deep(.track-row) { cursor: default; }
.track-list :deep(.track-row:not([data-ghost="true"]):hover) { background: var(--p-content-hover-background); }
.track-list :deep(.track-row[aria-selected="true"]) { background: var(--p-highlight-background) !important; color: var(--p-highlight-color); }
.track-list :deep(.track-row:focus-visible) { outline: 1px solid var(--p-primary-color); outline-offset: -1px; }
.track-list :deep(.track-row[data-upstream-missing="true"]),
.track-list :deep(.track-row[data-ghost="true"]) { color: var(--p-ghost-color); }
</style>
