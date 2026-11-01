import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'
import { emptyProblems, type TaskChange } from '@/api/events'
import { createRefreshQueue } from '@/lib/refreshQueue'

/** Problem-level read state. Drafts and approved scopes must not be reconstructed
 * from the selected index or silently expanded by a background snapshot. */
export const useReviewStore = defineStore('review', () => {
  const queue = ref<Models.app.ReviewProblemQueueView | null>(null)
  const selectedId = ref<number | null>(null)
  const loading = ref(false)
  const error = ref('')
  const items = computed(() => queue.value?.items ?? [])
  const problems = computed(() => queue.value?.problems ?? emptyProblems)
  const index = computed(() => items.value.findIndex(p => p.id === selectedId.value))
  const current = computed(() => items.value[index.value] ?? null)
  let subscriptions: (() => void)[] = []
  let minimumRevision = 0
  let lifecycle = 0

  function select(id: number) {
    if (items.value.some(p => p.id === id)) selectedId.value = id
  }

  function makeRefreshQueue() {
    return createRefreshQueue(async owns => {
      try {
        const next = await App.GetReviewProblemQueue()
        if (!owns()) return
        // An event can precede the IPC response, including across a remount.
        // Never restore a closed problem from an older committed snapshot.
        if (next.revision < Math.max(minimumRevision, queue.value?.revision ?? 0)) {
          error.value = '复核状态已更新，请刷新后重试'
          return
        }
        const previousIndex = Math.max(0, index.value)
        const id = selectedId.value
        queue.value = next
        selectedId.value = next.items.some(p => p.id === id)
          ? id
          : next.items[Math.min(previousIndex, next.items.length - 1)]?.id ?? null
        error.value = ''
      } catch (e) {
        // Transient failures retain content, selection and source ranges.
        if (owns()) error.value = String(e)
      }
    })
  }
  let refreshQueue = makeRefreshQueue()

  async function refresh() {
    const generation = lifecycle
    loading.value = true
    try { await refreshQueue.request() }
    finally { if (generation === lifecycle) loading.value = false }
  }

  function subscribe() {
    if (subscriptions.length) return
    const changed = (change: TaskChange) => {
      minimumRevision = Math.max(minimumRevision, change?.revision ?? 0)
      void refresh()
    }
    subscriptions = [
      EventsOn(Events.ReviewChanged, changed),
      EventsOn(Events.TasksChanged, changed),
      EventsOn(Events.ImportProgress, () => { void refresh() }),
      // Reprojects ownership and covers locally; never repeats upstream search.
      EventsOn(Events.LibraryChanged, () => { void refresh() }),
    ]
  }

  function unsubscribeAll() {
    lifecycle++
    subscriptions.forEach(off => off())
    subscriptions = []
    refreshQueue.dispose()
    refreshQueue = makeRefreshQueue()
    loading.value = false
  }

  return { queue, items, problems, selectedId, index, current, loading, error, select, refresh, subscribe, unsubscribeAll }
})
