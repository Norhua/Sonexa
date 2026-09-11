<script setup lang="ts">
/**
 * 侧栏：四个分组 —— 资料库、歌单、标签、任务。
 *
 * 上游搜索是独立入口，不置于资料库分组内。
 */
import {
  Library, Clock, Users, Disc3, ListMusic, Tag, Plus,
  Search, Settings, ListChecks, ChevronRight, Pause, Play,
} from 'lucide-vue-next'
import { useLibraryStore, type ViewKind } from '@/stores/library'
import { useTasksStore } from '@/stores/tasks'
import { useSettingsStore } from '@/stores/settings'

const library = useLibraryStore()
const tasks = useTasksStore()
const settings = useSettingsStore()

const emit = defineEmits<{
  newCollection: [kind: 'playlist' | 'tag']
  renameCollection: [id: number, kind: 'playlist' | 'tag', name: string]
  deleteCollection: [id: number, kind: 'playlist' | 'tag']
}>()

function isActive(kind: ViewKind, id = 0) {
  return library.view === kind && library.contextId === id
}

/** 「全部曲目」是虚拟视图，不占 collection 的行，也不可重命名或删除。 */
function onCollectionContext(event: MouseEvent, id: number, kind: 'playlist' | 'tag', name: string) {
  event.preventDefault()
  const action = window.prompt(`对「${name}」执行：输入新名称以重命名，留空并确认则删除`, name)
  if (action === null) return
  if (action.trim() === '') emit('deleteCollection', id, kind)
  else emit('renameCollection', id, kind, action.trim())
}
</script>

<template>
  <nav class="flex w-56 shrink-0 flex-col border-r bg-card text-sm">
    <div class="min-h-0 flex-1 overflow-auto py-2">
      <!-- 资料库 -->
      <section class="mb-3">
        <h2 class="px-3 py-1 text-xs font-medium text-muted-foreground">资料库</h2>
        <button
          v-for="item in [
            { kind: 'all' as ViewKind, label: '全部曲目', icon: Library },
            { kind: 'recent' as ViewKind, label: '最近添加', icon: Clock },
            { kind: 'artists' as ViewKind, label: '艺术家', icon: Users },
            { kind: 'albums' as ViewKind, label: '专辑', icon: Disc3 },
          ]"
          :key="item.kind"
          :class="[
            'flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent',
            isActive(item.kind) ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo(item.kind)"
        >
          <component :is="item.icon" class="size-4" />
          {{ item.label }}
        </button>
      </section>

      <!-- 上游搜索：独立入口 -->
      <section class="mb-3">
        <!-- 出网前置条件未满足时真正禁用，而不是仅仅调暗；
             原因说明与跳转设置的入口由全局提示位统一给出 -->
        <button
          :class="[
            'flex w-full items-center gap-2 px-3 py-1.5 text-left',
            settings.outboundAllowed ? 'hover:bg-accent' : 'cursor-not-allowed opacity-50',
            isActive('upstream') ? 'bg-accent font-medium' : '',
          ]"
          :disabled="!settings.outboundAllowed"
          :title="!settings.outboundAllowed ? settings.outboundReason : ''"
          @click="settings.outboundAllowed && library.goTo('upstream')"
        >
          <Search class="size-4" />
          上游搜索
        </button>
      </section>

      <!-- 歌单 -->
      <section class="mb-3">
        <div class="flex items-center justify-between px-3 py-1">
          <h2 class="text-xs font-medium text-muted-foreground">歌单</h2>
          <button class="rounded p-0.5 hover:bg-accent" title="新建歌单"
            @click="emit('newCollection', 'playlist')">
            <Plus class="size-3.5" />
          </button>
        </div>
        <button
          v-for="p in library.playlists"
          :key="p.id"
          :class="[
            'flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent',
            isActive('playlist', p.id) ? 'bg-accent font-medium' : '',
          ]"
          @click="library.goTo('playlist', { id: p.id, title: p.name })"
          @contextmenu="onCollectionContext($event, p.id, 'playlist', p.name)"
        >
          <ListMusic class="size-4 shrink-0" />
          <span class="flex-1 truncate">{{ p.name }}</span>
          <span class="text-xs text-muted-foreground">{{ p.trackCount }}</span>
        </button>
        <p v-if="library.playlists.length === 0" class="px-3 py-1 text-xs text-muted-foreground">
          还没有歌单
        </p>
      </section>

      <!-- 标签：以 chip 呈现 -->
      <section class="mb-3">
        <div class="flex items-center justify-between px-3 py-1">
          <h2 class="text-xs font-medium text-muted-foreground">标签</h2>
          <button class="rounded p-0.5 hover:bg-accent" title="新建标签"
            @click="emit('newCollection', 'tag')">
            <Plus class="size-3.5" />
          </button>
        </div>
        <div class="flex flex-wrap gap-1 px-3 py-1">
          <button
            v-for="t in library.tags"
            :key="t.id"
            :class="[
              'rounded-full border px-2 py-0.5 text-xs hover:bg-accent',
              isActive('tag', t.id) ? 'bg-accent font-medium' : '',
            ]"
            @click="library.goTo('tag', { id: t.id, title: t.name })"
            @contextmenu="onCollectionContext($event, t.id, 'tag', t.name)"
          >
            #{{ t.name }}
            <span class="text-muted-foreground">{{ t.trackCount }}</span>
          </button>
          <p v-if="library.tags.length === 0" class="text-xs text-muted-foreground">还没有标签</p>
        </div>
      </section>
    </div>

    <!-- 任务：常驻一条，点击展开详细进度与日志 -->
    <section class="shrink-0 border-t">
      <h2 class="px-3 pt-2 text-xs font-medium text-muted-foreground">任务</h2>

      <button
        v-if="tasks.summary && tasks.summary.total > 0"
        class="w-full px-3 py-1.5 text-left hover:bg-accent"
        @click="tasks.expanded = !tasks.expanded"
      >
        <div class="flex items-center gap-1.5">
          <ChevronRight :class="['size-3.5 transition-transform', tasks.expanded ? 'rotate-90' : '']" />
          <span class="flex-1">导入 {{ tasks.progressLabel }}</span>
        </div>
        <div class="mt-1 h-1 overflow-hidden rounded bg-muted">
          <div class="h-full bg-primary transition-all" :style="{ width: `${tasks.progressPercent}%` }" />
        </div>
      </button>

      <button
        :class="[
          'flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-accent',
          isActive('review') ? 'bg-accent font-medium' : '',
        ]"
        @click="library.goTo('review')"
      >
        <ListChecks class="size-4" />
        复核
        <span v-if="tasks.reviewPending > 0" class="rounded-full bg-primary px-1.5 text-xs text-primary-foreground">
          {{ tasks.reviewPending }}
        </span>
      </button>

      <!-- 展开后的详细进度与日志 -->
      <div v-if="tasks.expanded && tasks.summary" class="border-t px-3 py-2 text-xs">
        <div class="mb-1.5 flex items-center gap-2">
          <button
            v-if="tasks.summary.running"
            class="flex items-center gap-1 rounded border px-1.5 py-0.5 hover:bg-accent"
            @click="tasks.pause()"
          >
            <Pause class="size-3" /> 暂停
          </button>
          <button
            v-else-if="tasks.summary.total > tasks.summary.processed"
            class="flex items-center gap-1 rounded border px-1.5 py-0.5 hover:bg-accent"
            @click="tasks.resume()"
          >
            <Play class="size-3" /> 恢复
          </button>
        </div>
        <dl v-if="tasks.summary.stats" class="mb-2 grid grid-cols-2 gap-x-2 text-muted-foreground">
          <div>已入库 {{ tasks.summary.stats.imported }}</div>
          <div>待复核 {{ tasks.summary.stats.review }}</div>
          <div>排队中 {{ tasks.summary.stats.queued }}</div>
          <div>失败 {{ tasks.summary.stats.failed }}</div>
        </dl>
        <div class="max-h-40 space-y-0.5 overflow-auto font-mono text-[10px] leading-relaxed">
          <p v-for="(line, i) in tasks.log.slice(-60)" :key="i" class="whitespace-pre-wrap text-muted-foreground">
            {{ line.text }}
          </p>
        </div>
      </div>
    </section>

    <button
      :class="[
        'flex shrink-0 items-center gap-2 border-t px-3 py-2 text-left hover:bg-accent',
        isActive('settings') ? 'bg-accent font-medium' : '',
      ]"
      @click="library.goTo('settings')"
    >
      <Settings class="size-4" />
      设置
    </button>
  </nav>
</template>
