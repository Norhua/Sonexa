import { afterEach, describe, expect, it, vi } from 'vitest'
import { App, type Models } from '../../src/api'
import { useProblemLease } from '../../src/lib/useProblemLease'
vi.mock('../../src/api', () => ({ App: { AcquireReviewProblemLease: vi.fn(), RenewReviewProblemLease: vi.fn(), ReleaseReviewProblemLease: vi.fn() } }))
const scope = (id: number) => ({ version: 1, problemId: id, revision: 1, queryRevision: 1, candidateRevision: 1, members: [] }) as unknown as Models.store.ProblemScope
const held = (id: number) => ({ scope: { ...scope(id), leaseToken: `token-${id}` }, lease: { problemId: id, token: `token-${id}`, expiresAt: 90000 } }) as Models.store.ProblemInteraction
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
afterEach(() => { vi.useRealTimers(); vi.resetAllMocks() })
describe('problem lease lifecycle', () => {
  it('releases a late acquisition instead of taking over the newly selected problem', async () => {
    const old = deferred<Models.store.ProblemInteraction>()
    vi.mocked(App.AcquireReviewProblemLease).mockReturnValueOnce(old.promise).mockResolvedValueOnce(held(2))
    const state = useProblemLease()
    const first = state.open(scope(1)); await Promise.resolve()
    await state.open(scope(2))
    old.resolve(held(1)); await first
    expect(state.scope.value?.problemId).toBe(2)
    expect(App.ReleaseReviewProblemLease).toHaveBeenCalledWith(held(1).lease)
    await state.close()
    expect(App.ReleaseReviewProblemLease).toHaveBeenCalledWith(held(2).lease)
  })
  it('renews without replacing scope revisions, and stops after close', async () => {
    vi.useFakeTimers()
    vi.mocked(App.AcquireReviewProblemLease).mockResolvedValue(held(1))
    vi.mocked(App.RenewReviewProblemLease).mockResolvedValue({ ...held(1).lease, expiresAt: 120000 })
    const state = useProblemLease(); await state.open(scope(1))
    state.update({ ...scope(1), revision: 9 } as Models.store.ProblemScope)
    await vi.advanceTimersByTimeAsync(30000)
    expect(App.RenewReviewProblemLease).toHaveBeenCalledTimes(1)
    expect(state.scope.value?.revision).toBe(9)
    expect(state.update({ ...scope(1), revision: 99, leaseToken: 'old-token' } as Models.store.ProblemScope)).toBe(false)
    expect(state.scope.value?.revision).toBe(9)
    await state.close(); await vi.advanceTimersByTimeAsync(90000)
    expect(App.RenewReviewProblemLease).toHaveBeenCalledTimes(1)
  })
  it('renew failure removes permission and retains an explicit error', async () => {
    vi.useFakeTimers()
    vi.mocked(App.AcquireReviewProblemLease).mockResolvedValue(held(1))
    vi.mocked(App.RenewReviewProblemLease).mockRejectedValue(new Error('expired'))
    const state = useProblemLease(); await state.open(scope(1))
    await vi.advanceTimersByTimeAsync(30000)
    expect(state.scope.value).toBeNull()
    expect(state.error.value).toContain('expired')
    expect(App.ReleaseReviewProblemLease).toHaveBeenCalledTimes(1)
    await state.close()
  })
})
