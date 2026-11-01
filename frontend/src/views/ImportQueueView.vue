<script setup lang="ts">
/**
 * 导入队列页。
 *
 * 任务从「侧栏里可展开的一小块」变成一个页面：进度、各状态计数、失败清单
 * 与匹配日志都在这里。侧栏只留一行常驻指示。
 *
 * 日志来自后端推送的进度事件（tasks store），不轮询——
 * 17 分钟的导入任务上轮询会产生数千次无谓的 IPC 调用。
 */
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { createRefreshQueue } from '@/lib/refreshQueue'
import { Pause, Play, Upload } from 'lucide-vue-next'
import { App, Events, EventsOn, type Models } from '@/api'
import EmptyState from '@/components/EmptyState.vue'
import Button from '@/volt/Button.vue'
import Message from '@/volt/Message.vue'
import ProgressBar from '@/volt/ProgressBar.vue'
import SecondaryButton from '@/volt/SecondaryButton.vue'
import { useSettingsStore } from '@/stores/settings'
import { useTasksStore } from '@/stores/tasks'
import { useUiStore } from '@/stores/ui'

const settings = useSettingsStore()
const tasks = useTasksStore()
const ui = useUiStore()

const emit = defineEmits<{ goReview: [] }>()

const batches = ref<Models.app.ImportBatchView[]>([])
const loading = ref(false)
const error = ref('')
const logBox = ref<HTMLElement | null>(null)

/** 当前批次：有正在跑的就是它，否则是最近的那一批。 */
const current = computed(
  () => batches.value.find((b) => b.running) ?? batches.value[0] ?? null,
)

const percent = computed(() => {
  const b = current.value
  if (!b || b.totalRows === 0) return 0
  return Math.round((b.processed / b.totalRows) * 100)
})

const offTasks = EventsOn(Events.TasksChanged, () => { void load() })
onMounted(() => void load())
onBeforeUnmount(() => { offTasks(); refreshQueue.dispose() })

// 状态、失败、批次及全局汇总也会变化，不能只监听 processed。
watch(() => tasks.summary, () => void load())

// 导入向导关掉就重读一次：刚提交的批次要立刻出现在这一页上
watch(
  () => ui.importOpen,
  (open) => {
    if (!open) void load()
  },
)

// 新日志到达时把日志区滚到底
watch(
  () => tasks.log.length,
  async () => {
    await Promise.resolve()
    if (logBox.value) logBox.value.scrollTop = logBox.value.scrollHeight
  },
)

const refreshQueue = createRefreshQueue(async current => {
  try {
    const next = (await App.GetImportQueue()) ?? []
    if (!current()) return
    batches.value = next
    error.value = ''
  } catch (e) {
    if (current()) error.value = String(e)
  } finally {
    if (current()) loading.value = false
  }
})

function load() {
  // 后台更新保留原内容；仅首次空页面展示加载态。
  if (!batches.value.length) loading.value = true
  return refreshQueue.request()
}

async function pause(id: number) {
  try {
    await App.PauseImport(id)
    await Promise.all([tasks.refresh(), load()])
  } catch (e) {
    error.value = String(e)
  }
}

async function resume(id: number) {
  try {
    await App.ResumeImport(id)
    await Promise.all([tasks.refresh(), load()])
  } catch (e) {
    error.value = String(e)
  }
}

function statusLabel(status: string): string {
  return { running: '进行中', paused: '已暂停', done: '已完成' }[status] ?? status
}

function basename(path: string): string {
  return path.split('/').pop() ?? path
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <header class="flex h-12 shrink-0 items-center gap-3 px-4" style="--wails-draggable: drag">
      <h2 class="text-xl font-bold">导入队列</h2>
      <div class="flex-1" />
      <Button
        size="small"
        style="--wails-draggable: no-drag"
        :disabled="!settings.outboundAllowed"
        :title="!settings.outboundAllowed ? settings.outboundReason : '从表格文件导入'"
        @click="ui.importOpen = true"
      >
        <Upload class="size-4" />
        导入表格
      </Button>
    </header>

    <EmptyState
      v-if="batches.length === 0 && !loading"
      title="还没有导入任务"
      description="从表格文件导入后，匹配进度与日志会显示在这里。"
      :actions="[{ label: '导入表格', key: 'import' }]"
      @action="ui.importOpen = true"
    />

    <div v-else class="min-h-0 flex-1 overflow-auto">
      <!-- 当前（或最近）批次的进度与计数 -->
      <section v-if="current" class="border-b p-4">
        <div class="flex items-center gap-3">
          <div class="min-w-0 flex-1">
            <p class="truncate text-sm font-medium" :title="current.filename">
              {{ basename(current.filename) }}
            </p>
            <p class="mt-0.5 text-xs text-muted-color">
              {{ statusLabel(current.status) }} ·
              {{ current.processed }}/{{ current.totalRows }}
              <!-- 无有效映射内容的行不进入进度分母；缺基本信息仍可复核。 -->
              <template v-if="current.fileRows > current.totalRows">
                （文件 {{ current.fileRows }} 行，{{ current.fileRows - current.totalRows }} 行无有效映射内容未入队）
              </template>
            </p>
          </div>
          <SecondaryButton
            v-if="current.running"
            variant="outlined"
            size="small"
            @click="pause(current.id)"
          >
            <Pause class="size-3.5" /> 暂停
          </SecondaryButton>
          <SecondaryButton
            v-else-if="current.processed < current.totalRows"
            variant="outlined"
            size="small"
            @click="resume(current.id)"
          >
            <Play class="size-3.5" /> 恢复
          </SecondaryButton>
        </div>

        <ProgressBar :value="percent" :show-value="false" class="mt-2 h-1.5" />

        <p class="mt-2 text-xs text-muted-color">进度以原始入队行为单位；复核 / 失败表示本轮已处理，不表示全部处置完成。</p>
        <dl class="mt-3 grid grid-cols-4 gap-2 text-xs">
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">新增收藏</dt>
            <dd class="text-base tabular-nums">{{ current.stats.imported }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">已存在跳过（行）</dt>
            <dd class="text-base tabular-nums">{{ current.stats.existingSkipped ?? 0 }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">待复核（行）</dt>
            <dd class="text-base tabular-nums">{{ current.stats.review }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">排队中（行）</dt>
            <dd class="text-base tabular-nums">{{ current.stats.queued }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">失败（行）</dt>
            <dd class="text-base tabular-nums">{{ current.stats.failed }}</dd>
          </div>
        </dl>

        <p class="mt-2 text-xs text-muted-color">匹配中 {{ current.stats.matching ?? 0 }} 行 · 其他跳过 {{ Math.max(0, (current.stats.skipped ?? 0) - (current.stats.existingSkipped ?? 0)) }} 行 · 已移出 {{ current.stats.discarded ?? 0 }} 行 · 未匹配 {{ current.stats.unmatched ?? 0 }} 行（待复核子集）</p>
        <p class="mt-2 text-xs">本批涉及 {{ current.stats.problems?.reviewProblems ?? 0 }} 个问题，{{ current.stats.problems?.sourceRows ?? 0 }} 行来源；全局 {{ tasks.reviewPending }} 个问题，涉及 {{ tasks.reviewSourceRows }} 行（跨批不重复计数）。</p>
        <SecondaryButton
          v-if="(current.stats.problems?.reviewProblems ?? 0) > 0"
          variant="outlined"
          size="small"
          class="mt-2"
          @click="emit('goReview')"
        >
          到复核队列 · 本批涉及 {{ current.stats.problems?.reviewProblems }} 个问题
        </SecondaryButton>

        <!-- 失败条目逐行列出原始输入，供用户核对 -->
        <div v-if="current.failedTitles.length > 0" class="mt-3">
          <h3 class="text-xs font-medium">失败条目</h3>
          <ul class="mt-1 space-y-0.5 text-xs text-muted-color">
            <li v-for="(t, i) in current.failedTitles" :key="i">{{ t }}</li>
          </ul>
        </div>
      </section>

      <!-- 匹配日志 -->
      <section class="border-b p-4">
        <h3 class="mb-1.5 text-xs font-medium">匹配日志</h3>
        <div
          ref="logBox"
          class="max-h-72 space-y-0.5 overflow-auto rounded border bg-surface-50 p-2 font-mono text-[11px] leading-relaxed dark:bg-surface-800"
        >
          <p v-if="tasks.log.length === 0" class="text-muted-color">
            本次会话还没有日志。导入开始后逐条显示。
          </p>
          <p
            v-for="(line, i) in tasks.log"
            :key="i"
            class="whitespace-pre-wrap text-muted-color"
          >
            {{ line.text }}
          </p>
        </div>
      </section>

      <!-- 历史批次 -->
      <section v-if="batches.length > 1" class="p-4">
        <h3 class="mb-1.5 text-xs font-medium">历史批次</h3>
        <table class="w-full border-collapse text-xs">
          <thead>
            <tr class="border-b text-left text-muted-color">
              <th class="px-2 py-1">文件</th>
              <th class="w-20 px-2 py-1">状态</th>
              <th class="w-20 px-2 py-1">新增收藏</th>
              <th class="w-24 px-2 py-1">已存在跳过（行）</th>
              <th class="w-20 px-2 py-1">待复核（行）</th>
              <th class="w-20 px-2 py-1">问题数</th>
              <th class="w-16 px-2 py-1">失败（行）</th>
              <th class="w-36 px-2 py-1">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in batches" :key="b.id" class="border-b" :data-batch-id="b.id">
              <td class="max-w-0 truncate px-2 py-1" :title="b.filename">{{ basename(b.filename) }}</td>
              <td class="px-2 py-1">{{ statusLabel(b.status) }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.imported }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.existingSkipped ?? 0 }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.review }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.problems?.reviewProblems ?? 0 }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.failed }}</td>
              <td class="px-2 py-1">{{ new Date(b.createdAt).toLocaleString() }}</td>
            </tr>
          </tbody>
        </table>
      </section>
    </div>

    <Message v-if="error" severity="error" size="small" class="shrink-0 rounded-none">
      {{ error }}
    </Message>
  </div>
</template>
