<script setup lang="ts">
/**
 * 侧栏导航。
 *
 * 形态对齐 Apple Music：分组标题 + 缩进的二级项，红绿灯落在侧栏顶部，
 * 底部只常驻「设置」。
 *
 * 顶部留出的那段高度不是装饰：macOS 用 TitleBarHiddenInset 时红绿灯浮在
 * 内容之上，不留位置就会压住第一行。这段区域同时设为可拖拽窗口区。
 */
import {
  Library, Clock, Users, Disc3, ListMusic, Tag, Plus,
  Search, Settings, ListChecks, Upload,
} from 'lucide-vue-next'
import { onMounted, onUnmounted, ref } from 'vue'
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

/** 歌单与标签的右键菜单。重命名与删除是两个动作，不该挤进一个确认框里二选一。 */
const menu = ref<{ x: number; y: number; id: number; kind: 'playlist' | 'tag'; name: string } | null>(null)

function onCollectionContext(
  event: MouseEvent,
  id: number,
  kind: 'playlist' | 'tag',
  name: string,
) {
  event.preventDefault()
  menu.value = { x: event.clientX, y: event.clientY, id, kind, name }
}

function closeMenu() {
  menu.value = null
}

function act(fn: () => void) {
  fn()
  closeMenu()
}

onMounted(() => window.addEventListener('pointerdown', closeMenu))
onUnmounted(() => window.removeEventListener('pointerdown', closeMenu))
</script>

<template>
  <nav class="flex w-60 shrink-0 flex-col border-r bg-card text-sm">
    <!-- 红绿灯安全区：与主区页头（PaneHeader）同高，
         使红绿灯与右侧的大标题落在同一条水平线上 -->
    <div class="h-12 shrink-0" style="--wails-draggable: drag" />

    <div class="min-h-0 flex-1 overflow-auto pb-2">
      <!-- 搜索：顶层入口，不属于任何分组 -->
      <button
        :class="[
          'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md px-2 py-1.5 text-left',
          isActive('search') ? 'bg-accent font-medium' : 'hover:bg-accent/60',
        ]"
        @click="library.goTo('search')"
      >
        <Search class="size-4 shrink-0" />
        搜索
      </button>

      <!-- 资料库 -->
      <section class="mt-4">
        <h2 class="px-4 pb-1 text-xs font-medium text-muted-foreground">资料库</h2>
        <button
          v-for="item in libraryItems"
          :key="item.kind"
          :class="[
            'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md py-1.5 pl-4 pr-2 text-left',
            item.kind === 'artists'
              ? isArtistsActive() && 'bg-accent font-medium'
              : item.kind === 'albums'
                ? isAlbumsActive() && 'bg-accent font-medium'
                : isActive(item.kind) && 'bg-accent font-medium',
            'hover:bg-accent/60',
          ]"
          @click="library.goTo(item.kind)"
        >
          <component :is="item.icon" class="size-4 shrink-0" />
          {{ item.label }}
        </button>
      </section>

      <!-- 歌单 -->
      <section class="mt-4">
        <div class="flex items-center justify-between px-4 pb-1">
          <h2 class="text-xs font-medium text-muted-foreground">歌单</h2>
          <button
            class="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
            title="新建歌单"
            @click="emit('newCollection', 'playlist')"
          >
            <Plus class="size-3.5" />
          </button>
        </div>
        <button
          v-for="p in library.playlists"
          :key="p.id"
          :class="[
            'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md py-1.5 pl-4 pr-2 text-left hover:bg-accent/60',
            isActive('playlist', p.id) ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo('playlist', { id: p.id, title: p.name })"
          @contextmenu="onCollectionContext($event, p.id, 'playlist', p.name)"
        >
          <ListMusic class="size-4 shrink-0" />
          <span class="flex-1 truncate">{{ p.name }}</span>
          <span class="text-xs text-muted-foreground">{{ p.trackCount }}</span>
        </button>
        <p v-if="library.playlists.length === 0" class="px-4 py-1 text-xs text-muted-foreground">
          还没有歌单
        </p>
      </section>

      <!-- 标签 -->
      <section class="mt-4">
        <div class="flex items-center justify-between px-4 pb-1">
          <h2 class="text-xs font-medium text-muted-foreground">标签</h2>
          <button
            class="rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
            title="新建标签"
            @click="emit('newCollection', 'tag')"
          >
            <Plus class="size-3.5" />
          </button>
        </div>
        <button
          v-for="t in library.tags"
          :key="t.id"
          :class="[
            'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md py-1.5 pl-4 pr-2 text-left hover:bg-accent/60',
            isActive('tag', t.id) ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo('tag', { id: t.id, title: t.name })"
          @contextmenu="onCollectionContext($event, t.id, 'tag', t.name)"
        >
          <Tag class="size-4 shrink-0" />
          <span class="flex-1 truncate">{{ t.name }}</span>
          <span class="text-xs text-muted-foreground">{{ t.trackCount }}</span>
        </button>
        <p v-if="library.tags.length === 0" class="px-4 py-1 text-xs text-muted-foreground">
          还没有标签
        </p>
      </section>

      <!-- 任务：两个页面，不是可展开的面板 -->
      <section class="mt-4">
        <h2 class="px-4 pb-1 text-xs font-medium text-muted-foreground">任务</h2>

        <button
          :class="[
            'mx-2 w-[calc(100%-1rem)] rounded-md py-1.5 pl-4 pr-2 text-left hover:bg-accent/60',
            isActive('importQueue') ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo('importQueue')"
        >
          <div class="flex items-center gap-2">
            <Upload class="size-4 shrink-0" />
            <span class="flex-1">导入队列</span>
            <span v-if="tasks.progressLabel" class="text-xs tabular-nums text-muted-foreground">
              {{ tasks.progressLabel }}
            </span>
          </div>
          <!-- 进度条常驻，但详细进度与日志在页面里 -->
          <div v-if="tasks.hasActiveImport" class="ml-6 mt-1 h-1 overflow-hidden rounded bg-muted">
            <div class="h-full bg-primary transition-all" :style="{ width: `${tasks.progressPercent}%` }" />
          </div>
        </button>

        <button
          :class="[
            'mx-2 flex w-[calc(100%-1rem)] items-center gap-2 rounded-md py-1.5 pl-4 pr-2 text-left hover:bg-accent/60',
            isActive('review') ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo('review')"
        >
          <ListChecks class="size-4 shrink-0" />
          <span class="flex-1">复核队列</span>
          <span
            v-if="tasks.reviewPending > 0"
            class="rounded-full bg-primary px-1.5 text-xs text-primary-foreground"
          >
            {{ tasks.reviewPending }}
          </span>
        </button>
      </section>
    </div>

    <!-- 底部只有设置，且它是弹窗而非页面 -->
    <button
      class="flex shrink-0 items-center gap-2 border-t px-4 py-2.5 text-left hover:bg-accent/60"
      @click="ui.openSettings()"
    >
      <Settings class="size-4 shrink-0" />
      设置
    </button>

    <!-- 歌单/标签的右键菜单 -->
    <div
      v-if="menu"
      class="fixed z-50 min-w-32 rounded-md border bg-popover py-1 text-sm shadow-md"
      :style="{ left: `${menu.x}px`, top: `${menu.y}px` }"
      @pointerdown.stop
    >
      <button
        class="block w-full px-3 py-1.5 text-left hover:bg-accent"
        @click="act(() => emit('renameCollection', menu!.id, menu!.kind))"
      >
        重命名…
      </button>
      <button
        class="block w-full px-3 py-1.5 text-left text-destructive hover:bg-accent"
        @click="act(() => emit('deleteCollection', menu!.id, menu!.kind))"
      >
        删除
      </button>
    </div>
  </nav>
</template>
