import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn } from '@/api'
import type { ImportProgress, TaskState } from '@/api/events'

/** 一条日志行。 */
export interface LogLine {
  at: Date
  text: string
}

/**
 * 侧栏任务区的状态。
 *
 * 进度由后端主动推送，前端不轮询——17 分钟的导入任务上轮询会产生
 * 数千次无谓的 IPC 调用。
 */
export const useTasksStore = defineStore('tasks', () => {
  const summary = ref<TaskState | null>(null)
  const log = ref<LogLine[]>([])
  const expanded = ref(false)
  let unsubscribe: (() => void) | null = null

  const hasActiveImport = computed(
    () => (summary.value?.total ?? 0) > 0 && (summary.value?.processed ?? 0) < (summary.value?.total ?? 0),
  )
  const reviewPending = computed(() => summary.value?.reviewPending ?? 0)
  const progressLabel = computed(() => {
    if (!summary.value || summary.value.total === 0) return ''
    return `${summary.value.processed}/${summary.value.total}`
  })
  const progressPercent = computed(() => {
    if (!summary.value || summary.value.total === 0) return 0
    return Math.round((summary.value.processed / summary.value.total) * 100)
  })

  /** 开始监听后端推送的进度事件。 */
  function subscribe() {
    if (unsubscribe) return
    unsubscribe = EventsOn(Events.ImportProgress, (p: ImportProgress) => {
      summary.value = {
        activeBatch: p.batchId,
        filename: summary.value?.filename ?? '',
        processed: p.processed,
        total: p.total,
        stats: p.stats,
        running: !p.done && !p.paused,
        reviewPending: p.stats?.review ?? 0,
      }
      if (p.line) {
        log.value.push({ at: new Date(), text: p.line })
        // 只保留最近 500 行，避免长任务把内存占满
        if (log.value.length > 500) log.value.splice(0, log.value.length - 500)
      }
    })
  }

  function unsubscribeAll() {
    unsubscribe?.()
    unsubscribe = null
  }

  async function refresh() {
    try {
      const s = await App.GetTaskSummary()
      if (!s) return
      summary.value = {
        activeBatch: s.activeBatch,
        filename: s.filename,
        processed: s.processed,
        total: s.total,
        stats: s.stats,
        running: s.running,
        reviewPending: s.reviewPending,
      }
    } catch {
      // 后端尚未就绪时静默跳过
    }
  }

  async function pause() {
    if (summary.value?.activeBatch) await App.PauseImport(summary.value.activeBatch)
    await refresh()
  }

  async function resume() {
    if (summary.value?.activeBatch) await App.ResumeImport(summary.value.activeBatch)
    await refresh()
  }

  return {
    summary,
    log,
    expanded,
    hasActiveImport,
    reviewPending,
    progressLabel,
    progressPercent,
    subscribe,
    unsubscribeAll,
    refresh,
    pause,
    resume,
  }
})
