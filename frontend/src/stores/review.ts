import { defineStore } from 'pinia'
import { ref } from 'vue'

/** 只保存本次应用会话的浏览游标；重进页面仍读取最新队列，不保留候选快照。 */
export const useReviewStore = defineStore('review', () => {
  const itemId = ref<number | null>(null)
  const index = ref(0)

  function remember(id: number | null, position: number) {
    itemId.value = id
    index.value = id === null ? 0 : position
  }

  /** ID 优先；条目消失时沿用原位置，末条消失则退到新的末条。 */
  function reconcile(items: readonly { id: number }[]) {
    const found = items.findIndex(item => item.id === itemId.value)
    const position = found >= 0 ? found : Math.max(0, Math.min(index.value, items.length - 1))
    remember(items[position]?.id ?? null, position)
    return position
  }

  return { itemId, index, remember, reconcile }
})
