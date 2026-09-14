import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'

/**
 * 「补全信息匹配」的状态。
 *
 * 入库不再同步补详情（那在 MB 1 req/s 之下就是 1–2 秒的卡顿），
 * 详情由后端的后台任务在空闲时逐条补上。这里只负责把它的状态显示出来，
 * 并提供开始/暂停。
 *
 * 状态由后端推送，前端不轮询——几百条的任务上轮询只会产生无谓的 IPC 调用。
 */
export const useEnrichStore = defineStore('enrich', () => {
  const status = ref<Models.main.EnrichStatus | null>(null)
  const error = ref('')
  let unsubscribe: (() => void) | null = null

  /** running 与 waiting 都算「在跑」：让路是任务自己的事，按钮该显示「暂停」。 */
  const running = computed(
    () => status.value?.state === 'running' || status.value?.state === 'waiting',
  )
  const done = computed(() => status.value?.done ?? 0)
  const total = computed(() => status.value?.total ?? 0)
  /** 工具栏上的 `xx/xx`。 */
  const label = computed(() => `${done.value}/${total.value}`)
  /** 没有任何需要补全的曲目时按钮无事可做。 */
  const idleEmpty = computed(() => total.value === 0)

  /** 悬浮提示：说明它此刻在等什么或最近为什么失败。 */
  const hint = computed(() => {
    const s = status.value
    if (!s) return '补全信息匹配'
    if (s.state === 'waiting') return s.reason || '正在让路'
    if (s.state === 'stalled') return s.reason || s.lastError || '已停下并保留进度'
    if (s.lastError) return `最近一次失败: ${s.lastError}`
    if (s.skipped > 0) return `无需匹配 ${s.skipped} 条不计入`
    return '补全信息匹配'
  })

  function subscribe() {
    if (unsubscribe) return
    unsubscribe = EventsOn(Events.EnrichProgress, (s: Models.main.EnrichStatus) => {
      status.value = s
    })
  }

  function unsubscribeAll() {
    unsubscribe?.()
    unsubscribe = null
  }

  async function refresh() {
    try {
      status.value = await App.GetEnrichStatus()
    } catch {
      // 后端尚未就绪时静默跳过
    }
  }

  async function toggle() {
    error.value = ''
    try {
      if (running.value) await App.PauseEnrich()
      else await App.StartEnrich()
      await refresh()
    } catch (e) {
      error.value = String(e)
    }
  }

  return {
    status,
    error,
    running,
    done,
    total,
    label,
    idleEmpty,
    hint,
    subscribe,
    unsubscribeAll,
    refresh,
    toggle,
  }
})
