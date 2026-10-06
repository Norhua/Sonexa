<script setup lang="ts">
/**
 * 侧栏导航。
 *
 * 形态对齐 Apple Music：分组标题 + 缩进的二级项，红绿灯落在侧栏顶部，
 * 底部常驻「设置」，全局提示位于设置上方。
 *
 * 顶部留出的那段高度不是装饰：macOS 用 TitleBarHiddenInset 时红绿灯浮在
 * 内容之上，不留位置就会压住第一行。这段区域同时设为可拖拽窗口区。
 */
import {
  Library, Clock, Users, Disc3, ListMusic, Tag, Plus,
  Search, Settings, ListChecks, Upload,
} from 'lucide-vue-next'
import { computed, ref } from 'vue'
import type { MenuItem } from 'primevue/menuitem'
import Badge from '@/volt/Badge.vue'
import ContextMenu from '@/volt/ContextMenu.vue'
import ProgressBar from '@/volt/ProgressBar.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useLibraryStore, type ViewKind } from '@/stores/library'
import { useTasksStore } from '@/stores/tasks'
import { useUiStore } from '@/stores/ui'

const library = useLibraryStore()
const tasks = useTasksStore()
const ui = useUiStore()

const emit = defineEmits<{
  newCollection: [kind: 'playlist' | 'tag']
  renameCollection: [id: number, kind: 'playlist' | 'tag']
  deleteCollection: [id: number, kind: 'playlist' | 'tag']
}>()

/** 资料库分组的固定条目。顺序：最近添加 → 全部曲目 → 艺术家 → 专辑。 */
const libraryItems: { kind: ViewKind; label: string; icon: unknown }[] = [
  { kind: 'recent', label: '最近添加', icon: Clock },
  { kind: 'all', label: '全部曲目', icon: Library },
  { kind: 'artists', label: '艺术家', icon: Users },
  { kind: 'albums', label: '专辑', icon: Disc3 },
]

/** 导航项的类名：选中态取 highlight，与曲目表格的选中行同一套令牌。 */
function itemClass(active: boolean, indent = true) {
  return [
    'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md py-1.5 pr-2 text-left',
    indent ? 'pl-4' : 'px-2',
    active ? 'bg-highlight font-medium' : 'hover:bg-emphasis',
  ]
}

function isActive(kind: ViewKind, id = 0) {
  return library.view === kind && library.contextId === id
}

/** 艺术家视图下钻到某位艺术家后，侧栏的「艺术家」仍应保持高亮。 */
function isArtistsActive() {
  return library.view === 'artists' || library.view === 'artist'
}

function isAlbumsActive() {
  return library.view === 'albums' || library.view === 'album'
}

function isLibraryItemActive(kind: ViewKind) {
  if (kind === 'artists') return isArtistsActive()
  if (kind === 'albums') return isAlbumsActive()
  return isActive(kind)
}

/** 歌单与标签的右键菜单。重命名与删除是两个动作，不该挤进一个确认框里二选一。 */
const menu = ref<InstanceType<typeof ContextMenu> | null>(null)
const target = ref<{ id: number; kind: 'playlist' | 'tag' } | null>(null)

const menuModel = computed<MenuItem[]>(() => {
  const t = target.value
  if (!t) return []
  return [
    { label: '重命名…', command: () => emit('renameCollection', t.id, t.kind) },
    {
      label: '删除',
      class: 'text-danger-color',
      command: () => emit('deleteCollection', t.id, t.kind),
    },
  ]
})

function onCollectionContext(event: MouseEvent, id: number, kind: 'playlist' | 'tag') {
  target.value = { id, kind }
  menu.value?.show(event)
}
</script>

<template>
  <nav class="flex w-60 shrink-0 flex-col border-r bg-app-chrome text-sm">
    <!-- 红绿灯安全区：与主区页头（PaneHeader）同高，
         使红绿灯与右侧的大标题落在同一条水平线上 -->
    <div class="h-12 shrink-0" style="--wails-draggable: drag" />

    <div class="min-h-0 flex-1 overflow-auto pb-2">
      <!-- 搜索：顶层入口，不属于任何分组 -->
      <button :class="itemClass(isActive('search'), false)" @click="library.goTo('search')">
        <Search class="size-4 shrink-0" />
        搜索
      </button>

      <!-- 资料库 -->
      <section class="mt-4">
        <h2 class="px-4 pb-1 text-xs font-medium text-muted-color">资料库</h2>
        <button
          v-for="item in libraryItems"
          :key="item.kind"
          :class="itemClass(isLibraryItemActive(item.kind))"
          @click="library.goTo(item.kind)"
        >
          <component :is="item.icon" class="size-4 shrink-0" />
          {{ item.label }}
        </button>
      </section>

      <!-- 歌单 -->
      <section class="mt-4">
        <div class="sidebar-section-header flex items-center justify-between px-4 pb-1">
          <h2 class="text-xs font-medium text-muted-color">歌单</h2>
          <SecondaryButton
            class="sidebar-section-action"
            variant="text"
            rounded
            size="small"
            title="新建歌单"
            aria-label="新建歌单"
            @click="emit('newCollection', 'playlist')"
          >
            <Plus class="size-3.5" />
          </SecondaryButton>
        </div>
        <button
          v-for="p in library.playlists"
          :key="p.id"
          :class="itemClass(isActive('playlist', p.id))"
          @click="library.goTo('playlist', { id: p.id, title: p.name })"
          @contextmenu.prevent="onCollectionContext($event, p.id, 'playlist')"
        >
          <ListMusic class="size-4 shrink-0" />
          <span class="flex-1 truncate">{{ p.name }}</span>
          <span class="text-xs text-muted-color">{{ p.trackCount }}</span>
        </button>
        <p v-if="library.playlists.length === 0" class="pl-6 pr-4 py-1 text-xs text-muted-color">
          还没有歌单
        </p>
      </section>

      <!-- 标签 -->
      <section class="mt-4">
        <div class="sidebar-section-header flex items-center justify-between px-4 pb-1">
          <h2 class="text-xs font-medium text-muted-color">标签</h2>
          <SecondaryButton
            class="sidebar-section-action"
            variant="text"
            rounded
            size="small"
            title="新建标签"
            aria-label="新建标签"
            @click="emit('newCollection', 'tag')"
          >
            <Plus class="size-3.5" />
          </SecondaryButton>
        </div>
        <button
          v-for="t in library.tags"
          :key="t.id"
          :class="itemClass(isActive('tag', t.id))"
          @click="library.goTo('tag', { id: t.id, title: t.name })"
          @contextmenu.prevent="onCollectionContext($event, t.id, 'tag')"
        >
          <Tag class="size-4 shrink-0" />
          <span class="flex-1 truncate">{{ t.name }}</span>
          <span class="text-xs text-muted-color">{{ t.trackCount }}</span>
        </button>
        <p v-if="library.tags.length === 0" class="pl-6 pr-4 py-1 text-xs text-muted-color">
          还没有标签
        </p>
      </section>

      <!-- 任务：两个页面，不是可展开的面板 -->
      <section class="mt-4">
        <h2 class="px-4 pb-1 text-xs font-medium text-muted-color">任务</h2>

        <button
          :class="[
            'mx-2 w-[calc(100%-1rem)] rounded-md py-1.5 pl-4 pr-2 text-left',
            isActive('importQueue') ? 'bg-highlight font-medium' : 'hover:bg-emphasis',
          ]"
          @click="library.goTo('importQueue')"
        >
          <div class="flex items-center gap-2">
            <Upload class="size-4 shrink-0" />
            <span class="flex-1">导入队列</span>
            <span v-if="tasks.progressLabel" class="text-xs tabular-nums text-muted-color">
              {{ tasks.progressLabel }}
            </span>
          </div>
          <!-- 进度条常驻，但详细进度与日志在页面里 -->
          <ProgressBar
            v-if="tasks.hasActiveImport"
            :value="tasks.progressPercent"
            :show-value="false"
            pt:root:class="ml-6 mt-1 h-1"
          />
        </button>

        <button
          :class="itemClass(isActive('review'))"
          @click="library.goTo('review')"
        >
          <ListChecks class="size-4 shrink-0" />
          <span class="flex-1">复核队列</span>
          <Badge v-if="tasks.reviewPending > 0" :value="tasks.reviewPending" size="small" />
        </button>
      </section>
    </div>

    <!-- 全局提示固定在设置上方；长提示单独滚动，不挤出设置入口。 -->
    <div class="max-h-[30vh] shrink-0 overflow-y-auto">
      <slot name="notice" />
    </div>

    <!-- 设置固定在底部，且它是弹窗而非页面 -->
    <button
      class="flex shrink-0 items-center gap-2 border-t px-4 py-2.5 text-left hover:bg-emphasis"
      @click="ui.openSettings()"
    >
      <Settings class="size-4 shrink-0" />
      设置
    </button>

    <!-- 歌单/标签的右键菜单 -->
    <ContextMenu ref="menu" :model="menuModel" />
  </nav>
</template>

<style scoped>
/* 仅标题行触发，不因悬停整个分组而显现；保留占位避免布局跳动。
   不用 visibility/display 隐藏，键盘仍可 Tab 聚焦；无 hover 的设备保留入口。 */
@media (hover: hover) {
  .sidebar-section-action { opacity: 0; pointer-events: none; }
  .sidebar-section-header:hover .sidebar-section-action,
  .sidebar-section-header:focus-within .sidebar-section-action {
    opacity: 1;
    pointer-events: auto;
  }
}
</style>
