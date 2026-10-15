import { describe, expect, it, vi } from 'vitest'
import { createRefreshQueue } from '../../src/lib/refreshQueue'

export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}

describe('event refresh queue', () => {
  it('coalesces bursts, invalidates older snapshots and reads once more after in-flight events', async () => {
    const first = deferred<void>()
    const valid: boolean[] = []
    const work = vi.fn(async (current: () => boolean) => {
      if (work.mock.calls.length === 1) await first.promise
      valid.push(current())
    })
    const queue = createRefreshQueue(work)
    const done = queue.request()
    void queue.request()
    await Promise.resolve()
    expect(work).toHaveBeenCalledTimes(1)
    for (let i = 0; i < 10; i++) void queue.request()
    first.resolve()
    await done
    expect(work).toHaveBeenCalledTimes(2)
    expect(valid).toEqual([false, true])
  })

  it('an event arriving at completion starts another read instead of falling between cleanup microtasks', async () => {
    const first = deferred<void>()
    let second!: Promise<void>
    const work = vi.fn(async () => {
      if (work.mock.calls.length === 1) {
        void first.promise.then(() => {
          queueMicrotask(() => queueMicrotask(() => { second = queue.request() }))
        })
        await first.promise
      }
    })
    const queue = createRefreshQueue(work)
    const done = queue.request()
    await Promise.resolve()
    first.resolve()
    await done
    await second
    expect(work).toHaveBeenCalledTimes(2)
  })

  it('disposal invalidates the pending read and stops queued work', async () => {
    const first = deferred<void>()
    let valid = true
    const work = vi.fn(async (current: () => boolean) => { await first.promise; valid = current() })
    const queue = createRefreshQueue(work)
    const done = queue.request()
    await Promise.resolve()
    void queue.request()
    queue.dispose()
    first.resolve()
    await done
    await queue.request()
    expect(valid).toBe(false)
    expect(work).toHaveBeenCalledTimes(1)
  })
})
