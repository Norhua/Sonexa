import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, type Models } from '../../src/api'
import { useLibraryStore } from '../../src/stores/library'

vi.mock('../../src/api', () => ({ App: { ListTracks: vi.fn(), ListCollections: vi.fn() } }))
function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: Error) => void
  const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej })
  return { promise, resolve, reject }
}
const page = (id: number) => ({ tracks: [{ id, title: `Track ${id}` }], total: 1 }) as Models.main.TrackPage
beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks() })

describe('tag filters', () => {
  it('renaming and deleting the active tag update its chip without erasing the query', async () => {
    vi.mocked(App.ListTracks).mockResolvedValue(page(1))
    const lib = useLibraryStore()
    await lib.pickTag(20)
    await lib.quickSearch('title')
    vi.mocked(App.ListCollections).mockResolvedValueOnce([]).mockResolvedValueOnce([{ id: 20, name: 'Renamed', kind: 'tag', trackCount: 1 }])
    await lib.loadCollections()
    expect(lib.chips.find(chip => chip.key === 'tagId')?.label).toBe('标签=Renamed')
    vi.mocked(App.ListCollections).mockResolvedValue([])
    await lib.loadCollections()
    expect(lib.filter.tagId).toBe(0)
    expect(lib.filter.query).toBe('title')
    expect(lib.chips.map(chip => chip.key)).toEqual(['query'])
  })
  it('intersects view, query and role; removing one chip preserves the others', async () => {
    vi.mocked(App.ListTracks).mockResolvedValue(page(1))
    const lib = useLibraryStore()
    await lib.goTo('playlist', { id: 10 })
    await lib.pickTag(20)
    await lib.quickSearch('title')
    expect(lib.baseFilter()).toMatchObject({ collectionId: 10, tagId: 20, query: 'title' })
    expect(lib.chips.map(chip => chip.key)).toEqual(['tagId', 'query'])
    await lib.removeChip('query')
    expect(lib.baseFilter().tagId).toBe(20)
    await lib.goTo('artists')
    expect(lib.filter.tagId).toBeUndefined()
    await lib.pickTag(20)
    await lib.goTo('artist', { id: 1 })
    await lib.applyFilter({ role: 'vocal' }, [{ key: 'role', label: '演唱' }])
    expect(lib.baseFilter()).toMatchObject({ artistId: 1, tagId: 20, role: 'vocal' })
    expect(lib.chips.map(chip => chip.key)).toEqual(['tagId', 'role'])
    await lib.goTo('artist', { id: 2 })
    expect(lib.filter.tagId).toBe(20)
    expect(lib.filter.role).toBeUndefined()
    await lib.pickTag(0)
    expect(lib.chips).toEqual([])
  })
})

describe('library request ownership', () => {
  it('keeps rows, count and loading owned by the latest artist request', async () => {
    const a = deferred<Models.main.TrackPage>(), b = deferred<Models.main.TrackPage>()
    vi.mocked(App.ListTracks).mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
    const lib = useLibraryStore()
    const first = lib.goTo('artist', { id: 1 })
    const second = lib.goTo('artist', { id: 2 })
    a.resolve(page(1)); await first
    expect(lib.loading).toBe(true)
    expect(lib.tracks).toEqual([])
    b.resolve(page(2)); await second
    expect(lib.contextId).toBe(2)
    expect(lib.tracks.map(t => t.id)).toEqual([2])
    expect(lib.total).toBe(1)
    expect(lib.loading).toBe(false)
  })
  it('ignores late successes and errors, including after leaving the table view', async () => {
    for (const reject of [false, true]) {
      const a = deferred<Models.main.TrackPage>()
      vi.mocked(App.ListTracks).mockReturnValueOnce(a.promise).mockResolvedValueOnce(page(2))
      const lib = useLibraryStore()
      const first = lib.goTo('artist', { id: 1 })
      await lib.goTo('artist', { id: 2 })
      if (reject) a.reject(new Error('old artist error'))
      else a.resolve(page(1))
      await first
      expect(lib.tracks.map(t => t.id)).toEqual([2])
      expect(lib.error).toBe('')
    }
    const pending = deferred<Models.main.TrackPage>()
    vi.mocked(App.ListTracks).mockReturnValueOnce(pending.promise)
    const lib = useLibraryStore()
    const load = lib.goTo('artist', { id: 3 })
    await lib.goTo('review')
    pending.resolve(page(3)); await load
    expect(lib.view).toBe('review')
    expect(lib.tracks).toEqual([])
    expect(lib.total).toBe(0)
    expect(lib.loading).toBe(false)
  })
  it('clears old-context rows but preserves rows during a same-context refresh', async () => {
    vi.mocked(App.ListTracks).mockResolvedValueOnce(page(1))
    const lib = useLibraryStore()
    await lib.goTo('artist', { id: 1 })
    const refresh = deferred<Models.main.TrackPage>()
    vi.mocked(App.ListTracks).mockReturnValueOnce(refresh.promise)
    const reloading = lib.loadTracks()
    expect(lib.tracks.map(t => t.id)).toEqual([1])
    refresh.resolve(page(1)); await reloading
    const change = deferred<Models.main.TrackPage>()
    vi.mocked(App.ListTracks).mockReturnValueOnce(change.promise)
    const switching = lib.goTo('artist', { id: 2 })
    expect(lib.tracks).toEqual([])
    expect(lib.total).toBe(0)
    change.reject(new Error('current error')); await switching
    expect(lib.error).toBe('Error: current error')
    expect(lib.loading).toBe(false)
  })
})
