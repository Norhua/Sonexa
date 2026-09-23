import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, EventsOn, type Models } from '../../src/api'
import { useCoverSearchStore } from '../../src/stores/coverSearch'

vi.mock('../../src/api', () => ({
  App: { CoverRevision: vi.fn(), CancelCoverSearch: vi.fn(), StartCoverSearch: vi.fn(), CoverSearchResults: vi.fn(), AdoptCoverCandidate: vi.fn() },
  Events: { CoverResults: 'cover:results' }, EventsOn: vi.fn(),
}))
const detail = (id: number) => ({ track: { id, releaseId: id, title: '曲目', album: '专辑', artist: '艺术家' } }) as Models.app.TrackDetail
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks()
  vi.mocked(App.CoverRevision).mockResolvedValue(0)
  vi.mocked(App.CancelCoverSearch).mockResolvedValue()
  vi.mocked(App.StartCoverSearch).mockResolvedValue()
  vi.mocked(App.CoverSearchResults).mockResolvedValue([])
  vi.mocked(EventsOn).mockReturnValue(vi.fn())
})
it('retains other source results and adoption authorization when retrying one source', async () => {
  const store = useCoverSearchStore()
  await store.open(detail(1)); await store.search()
  const requests = vi.mocked(App.StartCoverSearch).mock.calls.map(([r]) => r)
  const qq = requests.find(r => r.sources[0] === 'qq')!, caa = requests.find(r => r.sources[0] === 'caa')!
  const receive = vi.mocked(EventsOn).mock.calls[0]![1]
  receive({ generation: qq.generation, searchId: qq.searchId, releaseId: 1, source: 'qq', state: 'ok', cards: [{ source: 'qq', image: { id: 10, variants: [{ id: 20 }] } }] })
  await store.search('caa')
  expect(App.CancelCoverSearch).toHaveBeenCalledWith(caa.searchId)
  expect(App.CancelCoverSearch).not.toHaveBeenCalledWith(qq.searchId)
  expect(store.cards).toHaveLength(1)
  vi.mocked(App.AdoptCoverCandidate).mockResolvedValue({ revision: 1 } as Models.app.CoverAdoptionResult)
  await store.adopt(20)
  expect(App.AdoptCoverCandidate).toHaveBeenCalledWith(qq.searchId, 20, 0)
  await store.close()
  receive({ generation: qq.generation, searchId: qq.searchId, releaseId: 1, source: 'qq', state: 'ok', cards: [{ image: { variants: [] } }] })
  expect(store.target).toBeNull(); expect(store.cards).toEqual([])
})
it('cancels a late successful start and ignores stale generation or target events', async () => {
  const store = useCoverSearchStore()
  await store.open(detail(1))
  let resolve!: () => void
  vi.mocked(App.StartCoverSearch).mockReturnValue(new Promise(r => { resolve = r }))
  const searching = store.search('qq')
  await Promise.resolve(); await Promise.resolve()
  const request = vi.mocked(App.StartCoverSearch).mock.calls[0]![0]
  const receive = vi.mocked(EventsOn).mock.calls[0]![1]
  receive({ generation: request.generation - 1, searchId: request.searchId, releaseId: 1, source: 'qq', state: 'empty', cards: [] })
  expect(store.results.qq?.state).toBe('loading')
  receive({ generation: request.generation, searchId: request.searchId, releaseId: 2, source: 'qq', state: 'empty', cards: [] })
  expect(store.results.qq?.state).toBe('loading')
  await store.close()
  resolve(); await searching
  expect(App.CancelCoverSearch).toHaveBeenCalledTimes(2)
  expect(App.CancelCoverSearch).toHaveBeenLastCalledWith(request.searchId)
  expect(App.CoverSearchResults).not.toHaveBeenCalled()
})

it('ignores late opening and starting results after close', async () => {
  let resolve!: (value: number) => void
  vi.mocked(App.CoverRevision).mockReturnValue(new Promise(r => { resolve = r }))
  const store = useCoverSearchStore()
  const opening = store.open(detail(1))
  await Promise.resolve()
  await store.close(); resolve(5); await opening
  expect(store.ready).toBe(false); expect(store.target).toBeNull()
  vi.mocked(App.CoverRevision).mockResolvedValue(0)
  await store.open(detail(2))
  let reject!: (error: Error) => void
  vi.mocked(App.StartCoverSearch).mockReturnValue(new Promise((_, r) => { reject = r }))
  const searching = store.search('qq')
  await Promise.resolve(); await Promise.resolve()
  await store.close(); reject(new Error('late')); await searching
  expect(store.error).toBe(''); expect(store.results).toEqual({})
})
