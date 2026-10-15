import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn } from '@/api'
import { createRefreshQueue } from '@/lib/refreshQueue'
import type { ImportProgress, TaskState } from '@/api/events'

/** 一条日志行。 */
export interface LogLine {
  at: Date
  text: string
}

/**
 * 导入任务的进度与日志。
 *
 * 侧栏只读 progressLabel / progressPercent / reviewPending 三个常驻指示，
 * 详细进度与日志由「导入队列」页呈现。
 *
 * 进度由后端主动推送，前端不轮询——17 分钟的导入任务上轮询会产生
 * 数千次无谓的 IPC 调用。暂停与恢复按批次发起（见 ImportQueueView），
 * 因此这里不再提供只作用于「当前批次」的包装。
 */
export const useTasksStore = defineStore('tasks', () => {
  const summary = ref<TaskState | null>(null)
  const log = ref<LogLine[]>([])
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
        filename: summary.value?.activeBatch === p.batchId ? summary.value.filename : '',
        processed: p.processed,
        total: p.total,
        stats: p.stats,
        running: !p.done && !p.paused,
        // 批次统计不是全库待复核总数；保留上次汇总，事件后按全批次重新读取。
        reviewPending: summary.value?.reviewPending ?? 0,
      }
      void refresh()
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
    refreshQueue.dispose()
    refreshQueue = makeRefreshQueue()
  }

  function makeRefreshQueue() {
    return createRefreshQueue(async current => {
      try {
        const s = await App.GetTaskSummary()
        if (s && current()) summary.value = s
      } catch {
        // 后端尚未就绪/暂时失败时保留已有进度，下一次事件继续读取。
      }
    })
  }
  let refreshQueue = makeRefreshQueue()

  function refresh() {
    return refreshQueue.request()
  }

  return {
    summary,
    log,
    hasActiveImport,
    reviewPending,
    progressLabel,
    progressPercent,
    subscribe,
    unsubscribeAll,
    refresh,
  }
})
