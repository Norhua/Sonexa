import { onBeforeUnmount, watch, type Ref } from 'vue'
import { App, type Models } from '@/api'

// 多个候选表格各自保留预取租约，退出一个视图不取消其他表格的同键任务。
const consumers = new Map<symbol, Models.covers.Ref[]>()
function visibleKeys() { return [...new Set([...consumers.values()].flatMap(refs => refs.map(ref => ref.key)))] }

export function useCandidateCovers(rows: Ref<Models.app.CandidateRow[]>) {
  const owner = Symbol('candidate-covers')
  let generation = 0
  const stop = watch(() => rows.value.map(row => [row.releaseId, row.coverRevision, row.provider, row.releaseRef, row.releaseGroupRef]), async () => {
    const current = ++generation
    try {
      const refs = await App.BuildCoverRefs(rows.value)
      if (current !== generation) return
      consumers.set(owner, refs ?? [])
      await App.CancelCovers(visibleKeys())
      if (current === generation && refs?.length) await App.PrefetchCovers(refs)
    } catch { /* 预取只做暖机，实际图片请求负责显示失败原因。 */ }
  }, { immediate: true })
  onBeforeUnmount(() => {
    generation++
    stop()
    consumers.delete(owner)
    void App.CancelCovers(visibleKeys()).catch(() => {})
  })
}
