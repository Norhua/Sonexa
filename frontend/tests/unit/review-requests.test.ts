import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, Events, EventsOn, type Models } from '../../src/api'
import { useReviewStore } from '../../src/stores/review'

vi.mock('../../src/api', () => ({
  App: { GetReviewProblemQueue: vi.fn() },
  Events: { ReviewChanged: 'review:changed', TasksChanged: 'tasks:changed', ImportProgress: 'import:progress', LibraryChanged: 'library:changed' },
  EventsOn: vi.fn(),
}))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
function queue(revision: number, ids = [101, 202], sourceRows = 3) {
  return {
    revision, problems: { reviewProblems: ids.length, pendingProblems: ids.length, failedProblems: 0, sourceRows },
    items: ids.map(id => ({ id, status: 'pending', scope: { version: 1, problemId: id, revision, queryRevision: 1, candidateRevision: 1, members: [{ itemId: id + 1000, revision: 1, inputRevision: 1 }] }, query: null,
      sources: [{ id: id + 1000, revision: 1, batchTag: `batch-${revision}.csv`, rawRow: '["original"]', candidates: [] }],
    })),
  } as Models.app.ReviewProblemQueueView
}
let listeners: Record<string, (...args: any[]) => void>
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks(); listeners = {}
  vi.mocked(EventsOn).mockImplementation((event, cb) => { listeners[event] = cb; return () => { delete listeners[event] } })
})

describe('independent review problem read state', () => {
  it('uses stable problem IDs, not source IDs or positions, and reports distinct units', async () => {
    const store = useReviewStore()
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(1))
    await store.refresh(); store.select(202)
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(2, [202, 101], 4))
    await store.refresh()
    expect(store.current?.id).toBe(202)
    expect(store.index).toBe(0)
    expect(store.problems.reviewProblems).toBe(2)
    expect(store.problems.sourceRows).toBe(4)
    expect(store.current?.sources[0].batchTag).toBe('batch-2.csv')
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(3, [101], 1))
    await store.refresh()
    expect(store.current?.id).toBe(101)
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(4, [], 0))
    await store.refresh()
    expect(store.current).toBeNull()
    expect(store.selectedId).toBeNull()
  })

  it('coalesces events, rejects an earlier queue, and refreshes same-count membership', async () => {
    const store = useReviewStore(); store.subscribe(); store.subscribe()
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(1))
    await store.refresh(); store.select(202)
    const old = deferred<Models.app.ReviewProblemQueueView>(), latest = deferred<Models.app.ReviewProblemQueueView>()
    vi.mocked(App.GetReviewProblemQueue).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const done = store.refresh(); await Promise.resolve()
    listeners[Events.ReviewChanged]({ revision: 3 })
    listeners[Events.TasksChanged]({ revision: 3 })
    listeners[Events.LibraryChanged]()
    old.resolve(queue(2, [101], 1))
    await vi.waitFor(() => expect(App.GetReviewProblemQueue).toHaveBeenCalledTimes(3))
    expect(store.current?.id).toBe(202)
    latest.resolve(queue(3, [101, 202], 5)); await done
    expect(store.current?.sources[0].batchTag).toBe('batch-3.csv')
    expect(store.problems.sourceRows).toBe(5)
    expect(EventsOn).toHaveBeenCalledTimes(4)
    store.unsubscribeAll()
  })

  it('does not restore a terminal problem from a lower committed revision', async () => {
    const store = useReviewStore(); store.subscribe()
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(10, [], 0))
    await store.refresh()
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(9))
    await store.refresh()
    expect(store.items).toEqual([])
    expect(store.error).toContain('状态已更新')
    // Even a response newer than the retained snapshot must respect the event.
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(11))
    listeners[Events.ReviewChanged]({ revision: 12 })
    await store.refresh()
    expect(store.items).toEqual([])
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(12, [303], 1))
    await store.refresh()
    expect(store.current?.id).toBe(303)
    expect(store.error).toBe('')
    store.unsubscribeAll()
  })

  it('retains content on failure and invalidates successes/errors across remount', async () => {
    const store = useReviewStore(); store.subscribe()
    vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(1))
    await store.refresh(); store.select(202)
    vi.mocked(App.GetReviewProblemQueue).mockRejectedValueOnce(new Error('offline'))
    await store.refresh()
    expect(store.current?.id).toBe(202)
    expect(store.error).toContain('offline')
    for (const failure of [false, true]) {
      const old = deferred<Models.app.ReviewProblemQueueView>()
      vi.mocked(App.GetReviewProblemQueue).mockReturnValueOnce(old.promise)
      const done = store.refresh(); await Promise.resolve()
      store.unsubscribeAll()
      expect(listeners).toEqual({})
      store.subscribe()
      vi.mocked(App.GetReviewProblemQueue).mockResolvedValue(queue(4, [303], 1))
      await store.refresh()
      if (failure) old.reject(new Error('late error'))
      else old.resolve(queue(3, [], 0))
      await done
      expect(store.current?.id).toBe(303)
      expect(store.error).toBe('')
      expect(store.loading).toBe(false)
    }
    store.unsubscribeAll()
  })
})
