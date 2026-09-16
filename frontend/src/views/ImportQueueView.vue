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
import { computed, onMounted, ref, watch } from 'vue'
import { Pause, Play, Upload } from 'lucide-vue-next'
import { App, type Models } from '@/api'
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

const batches = ref<Models.main.ImportBatchView[]>([])
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

onMounted(load)

// 后端每推一条进度就刷新一次计数：页面开着时数字要跟着动
watch(() => tasks.summary?.processed, () => void refresh())

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

async function load() {
  loading.value = true
  error.value = ''
  try {
    batches.value = (await App.GetImportQueue()) ?? []
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

/** 轻量刷新：不打 loading 态，避免进度每跳一条就闪一次。 */
async function refresh() {
  try {
    batches.value = (await App.GetImportQueue()) ?? []
  } catch {
    // 刷新失败不覆盖页面上已有的内容
  }
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
              <!-- 缺曲名的行在提交时就跳过了，没进队列——说清楚免得对不上文件行数 -->
              <template v-if="current.fileRows > current.totalRows">
                （文件 {{ current.fileRows }} 行，{{ current.fileRows - current.totalRows }} 行缺曲名已跳过）
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

        <dl class="mt-3 grid grid-cols-4 gap-2 text-xs">
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">已入库</dt>
            <dd class="text-base tabular-nums">{{ current.stats.imported }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">待复核</dt>
            <dd class="text-base tabular-nums">{{ current.stats.review }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">排队中</dt>
            <dd class="text-base tabular-nums">{{ current.stats.queued }}</dd>
          </div>
          <div class="rounded border px-2 py-1.5">
            <dt class="text-muted-color">失败</dt>
            <dd class="text-base tabular-nums">{{ current.stats.failed }}</dd>
          </div>
        </dl>

        <SecondaryButton
          v-if="current.stats.review > 0"
          variant="outlined"
          size="small"
          class="mt-2"
          @click="emit('goReview')"
        >
          到复核队列处置 {{ current.stats.review }} 条
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
              <th class="w-20 px-2 py-1">已入库</th>
              <th class="w-20 px-2 py-1">待复核</th>
              <th class="w-16 px-2 py-1">失败</th>
              <th class="w-36 px-2 py-1">时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="b in batches" :key="b.id" class="border-b">
              <td class="max-w-0 truncate px-2 py-1" :title="b.filename">{{ basename(b.filename) }}</td>
              <td class="px-2 py-1">{{ statusLabel(b.status) }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.imported }}</td>
              <td class="px-2 py-1 tabular-nums">{{ b.stats.review }}</td>
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
