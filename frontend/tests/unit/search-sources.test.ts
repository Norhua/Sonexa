import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, type Models } from '../../src/api'
import { useCoverSearchStore } from '../../src/stores/coverSearch'
import { useLyricsSearchStore } from '../../src/stores/lyricsSearch'

vi.mock('../../src/api', () => ({ App: {
  ListCoverSources: vi.fn(), CoverRevision: vi.fn(), StartCoverSearch: vi.fn(), CancelCoverSearch: vi.fn(), CoverSearchResults: vi.fn(),
  ListLyricsSources: vi.fn(), LyricsRevision: vi.fn(), CreateLyricsSearch: vi.fn(), SearchLyrics: vi.fn(), CancelLyricsSearch: vi.fn(),
}, Events: { CoverResults: 'cover:results' }, EventsOn: vi.fn(() => vi.fn()) }))

const detail = (id: number) => ({ track: { id, releaseId: id, title: 'song' } }) as Models.app.TrackDetail
const deferred = <T>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
const list = (ids: string[]) => ids.map(id => ({ id, name: id, needsCountry: id === 'regional-only' }))

beforeEach(() => {
  setActivePinia(createPinia()); vi.clearAllMocks()
  vi.mocked(App.CoverRevision).mockResolvedValue(0)
  vi.mocked(App.LyricsRevision).mockResolvedValue(0)
  vi.mocked(App.CoverSearchResults).mockResolvedValue([])
  vi.mocked(App.SearchLyrics).mockResolvedValue([])
  vi.mocked(App.CreateLyricsSearch).mockResolvedValue('session')
})

for (const area of ['cover', 'lyrics'] as const) {
  describe(`${area} registered sources`, () => {
    const setup = () => ({
      store: area === 'cover' ? useCoverSearchStore() : useLyricsSearchStore(),
      load: vi.mocked(area === 'cover' ? App.ListCoverSources : App.ListLyricsSources),
      calls: () => area === 'cover'
        ? vi.mocked(App.StartCoverSearch).mock.calls.map(([r]) => r.sources[0])
        : vi.mocked(App.SearchLyrics).mock.calls.map(([, source]) => source),
    })

    it('defaults to the registered order, preserves valid choices and rejects removed retries', async () => {
      const { store, load, calls } = setup()
      load.mockResolvedValue(list(['single-only', 'regional-only']))
      await store.open(detail(1))
      expect(store.sourceOptions.map(s => s.id)).toEqual(['single-only', 'regional-only'])
      expect(store.sources).toEqual(['single-only', 'regional-only'])
      await store.search()
      expect(calls()).toEqual(['single-only', 'regional-only'])
      store.sources = ['regional-only', 'stale', 'regional-only']
      load.mockResolvedValue(list(['new-only', 'regional-only']))
      await store.open(detail(2))
      expect(store.sources).toEqual(['regional-only'])
      const before = calls().length
      await store.search('single-only')
      expect(calls()).toHaveLength(before)
      expect(store.error).toContain('未登记')
      await store.search()
      expect(calls().slice(before)).toEqual(['regional-only'])
      store.sources = ['stale']
      await store.search()
      expect(store.sources).toEqual([])
      expect(calls()).toHaveLength(before + 1)
      expect(store.error).toContain('请选择')
      store.sources = []
      await store.open(detail(3))
      expect(store.sources).toEqual([]) // An explicit empty selection is not reset to all.
    })

    it('never searches while loading, after a failed load, or with an empty registry', async () => {
      const { store, load, calls } = setup()
      const pending = deferred<ReturnType<typeof list>>()
      load.mockReturnValue(pending.promise)
      const opening = store.open(detail(1))
      await vi.waitFor(() => expect(load).toHaveBeenCalled())
      await store.search('single-only')
      expect(calls()).toEqual([])
      pending.resolve(list(['single-only'])); await opening
      load.mockRejectedValue(new Error('registry unavailable'))
      await store.open(detail(2)); await store.search('single-only')
      expect(store.ready).toBe(false)
      expect(store.sourceOptions).toEqual([])
      expect(store.error).toContain('registry unavailable')
      expect(calls()).toEqual([])
      load.mockResolvedValue([])
      await store.open(detail(3)); await store.search()
      expect(store.sources).toEqual([])
      expect(store.error).toContain('请选择')
      expect(calls()).toEqual([])
      expect(App.CreateLyricsSearch).not.toHaveBeenCalled()
    })

    it('ignores a source list that resolves after close or after a new target opens', async () => {
      const { store, load } = setup()
      const old = deferred<ReturnType<typeof list>>()
      load.mockReturnValueOnce(old.promise).mockResolvedValue(list(['current-only']))
      const opening = store.open(detail(1))
      await vi.waitFor(() => expect(load).toHaveBeenCalled())
      await store.open(detail(2))
      old.resolve(list(['old-only'])); await opening
      expect(store.target?.track.id).toBe(2)
      expect(store.sources).toEqual(['current-only'])
      const late = deferred<ReturnType<typeof list>>()
      load.mockReturnValue(late.promise)
      const closing = store.open(detail(3))
      await vi.waitFor(() => expect(load).toHaveBeenCalledTimes(3))
      await store.close(); late.resolve(list(['late-only'])); await closing
      expect(store.ready).toBe(false)
      expect(store.sourceOptions).toEqual([])
      expect(store.target).toBeNull()
    })
  })
}

it('shows country capability for any selected cover source and passes the country unchanged', async () => {
  vi.mocked(App.ListCoverSources).mockResolvedValue(list(['single-only', 'regional-only']))
  const store = useCoverSearchStore()
  await store.open(detail(1))
  expect(store.needsCountry).toBe(true)
  store.sources = ['single-only']; expect(store.needsCountry).toBe(false)
  store.sources = ['regional-only']; store.country = 'JP'
  expect(store.needsCountry).toBe(true)
  await store.search()
  expect(App.StartCoverSearch).toHaveBeenCalledWith(expect.objectContaining({ sources: ['regional-only'], country: 'JP' }))
})
