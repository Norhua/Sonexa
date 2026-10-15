import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, Events, EventsOn, type Models } from '../../src/api'
import { useTasksStore } from '../../src/stores/tasks'
import { useEnrichStore } from '../../src/stores/enrich'

vi.mock('../../src/api', () => ({
  App: { GetTaskSummary: vi.fn(), GetEnrichStatus: vi.fn() },
  Events: { ImportProgress: 'import:progress', EnrichProgress: 'enrich:progress' }, EventsOn: vi.fn(),
}))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}
const summary = (processed: number, reviewPending: number) => ({ activeBatch: 2, filename: 'new.csv', processed, total: 10, stats: { review: 1, failed: 2 }, running: true, reviewPending }) as Models.app.TaskSummary
let listeners: Record<string, (...args: any[]) => void>
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks(); listeners = {}
  vi.mocked(EventsOn).mockImplementation((event, cb) => { listeners[event] = cb; return () => { delete listeners[event] } })
})

describe('task progress and global review totals', () => {
  it('never uses batch review as global total and rejects a summary older than an event', async () => {
    const store = useTasksStore()
    store.subscribe()
    vi.mocked(App.GetTaskSummary).mockResolvedValue(summary(1, 9))
    await store.refresh()
    const old = deferred<Models.app.TaskSummary>(), latest = deferred<Models.app.TaskSummary>()
    vi.mocked(App.GetTaskSummary).mockReturnValueOnce(old.promise).mockReturnValueOnce(latest.promise)
    const done = store.refresh()
    await Promise.resolve()
    listeners[Events.ImportProgress]({ batchId: 2, processed: 5, total: 10, stats: { review: 1, failed: 2 }, done: false, paused: false })
    expect(store.summary?.processed).toBe(5)
    expect(store.reviewPending).toBe(9)
    old.resolve(summary(2, 3))
    await vi.waitFor(() => expect(App.GetTaskSummary).toHaveBeenCalledTimes(3))
    expect(store.summary?.processed).toBe(5)
    expect(store.reviewPending).toBe(9)
    latest.resolve(summary(5, 11))
    await done
    expect(store.reviewPending).toBe(11)
    expect(store.summary?.processed).toBe(5)
    store.unsubscribeAll()
  })

  it('preserves latest progress on failure, retries, and ignores replies after unsubscribe', async () => {
    const store = useTasksStore()
    store.subscribe()
    vi.mocked(App.GetTaskSummary).mockResolvedValue(summary(1, 7))
    await store.refresh()
    vi.mocked(App.GetTaskSummary).mockRejectedValueOnce(new Error('offline'))
    await store.refresh()
    expect(store.reviewPending).toBe(7)
    const old = deferred<Models.app.TaskSummary>()
    vi.mocked(App.GetTaskSummary).mockReturnValueOnce(old.promise)
    const done = store.refresh(); await Promise.resolve()
    store.unsubscribeAll()
    old.resolve(summary(9, 0)); await done
    expect(store.reviewPending).toBe(7)
    expect(listeners).toEqual({})
    store.subscribe()
    vi.mocked(App.GetTaskSummary).mockResolvedValue(summary(9, 12))
    await store.refresh()
    expect(store.reviewPending).toBe(12)
  })

  it('enrichment summary cannot replace a newer progress event', async () => {
    const store = useEnrichStore()
    store.subscribe()
    const old = deferred<Models.app.EnrichStatus>()
    vi.mocked(App.GetEnrichStatus).mockReturnValueOnce(old.promise)
    const done = store.refresh(); await Promise.resolve()
    listeners[Events.EnrichProgress]({ state: 'running', done: 4, total: 10, pending: 6 })
    old.resolve({ state: 'running', done: 1, total: 10, pending: 9 } as Models.app.EnrichStatus)
    await done
    expect(store.done).toBe(4)
  })
})
