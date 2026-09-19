import { computed, ref, watch, type Ref } from 'vue'

interface Entry { key: string; ghost?: boolean }
interface Modifiers { shiftKey?: boolean; ctrlKey?: boolean; metaKey?: boolean }

/**
 * 文件列表唯一的选择状态机。锚点是稳定 key，不是排序前的数组下标。
 * 不依赖 DOM/PrimeVue 私有字段；rows 始终是屏幕上的顺序。
 */
export function useListSelection<T extends Entry>(rows: Ref<T[]>) {
  const selected = ref(new Set<string>())
  const anchor = ref<string | null>(null)
  const focused = ref<string | null>(null)
  const selectable = computed(() => rows.value.filter(row => !row.ghost))
  const selectedRows = computed(() => selectable.value.filter(row => selected.value.has(row.key)))
  let rangeBase: Set<string> | null = null

  watch(selectable, liveRows => {
    const live = new Set(liveRows.map(row => row.key))
    selected.value = new Set([...selected.value].filter(key => live.has(key)))
    if (anchor.value && !live.has(anchor.value)) anchor.value = null
    if (!focused.value || !live.has(focused.value)) focused.value = liveRows[0]?.key ?? null
    // 保持同一锚点，但排序/数据更新后新的范围基于当前选中集合。
    rangeBase = null
  }, { immediate: true, flush: 'sync' })

  function choose(key: string, event: Modifiers = {}) {
    const ids = selectable.value.map(row => row.key)
    if (!ids.includes(key)) return
    const additive = !!(event.ctrlKey || event.metaKey)
    if (event.shiftKey) {
      const start = anchor.value ?? focused.value ?? key
      anchor.value = start
      const from = ids.indexOf(start), to = ids.indexOf(key)
      if (rangeBase === null) rangeBase = additive ? new Set(selected.value) : new Set()
      selected.value = new Set([...rangeBase, ...ids.slice(Math.min(from, to), Math.max(from, to) + 1)])
    } else {
      rangeBase = null
      anchor.value = key
      if (additive) {
        const next = new Set(selected.value)
        if (next.has(key)) next.delete(key)
        else next.add(key)
        selected.value = next
      } else selected.value = new Set([key])
    }
    focused.value = key
  }

  function move(direction: 'up' | 'down' | 'first' | 'last', event: Modifiers = {}) {
    const ids = selectable.value.map(row => row.key)
    if (!ids.length) return
    const current = Math.max(0, ids.indexOf(focused.value ?? ''))
    const index = direction === 'first' ? 0 : direction === 'last' ? ids.length - 1
      : Math.max(0, Math.min(ids.length - 1, current + (direction === 'up' ? -1 : 1)))
    if ((event.ctrlKey || event.metaKey) && !event.shiftKey) focused.value = ids[index]
    else choose(ids[index], event)
  }

  function all() {
    selected.value = new Set(selectable.value.map(row => row.key))
    anchor.value = focused.value
    rangeBase = null
  }

  function clear() {
    selected.value = new Set()
    anchor.value = null
    rangeBase = null
  }

  function context(key: string) {
    if (!selectable.value.some(row => row.key === key)) return
    if (!selected.value.has(key)) choose(key)
    focused.value = key
  }

  return { selected, selectedRows, focused, anchor, choose, move, all, clear, context }
}
