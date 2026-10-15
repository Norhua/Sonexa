import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, type Models } from '../../src/api'
import { useSearchStore } from '../../src/stores/search'

vi.mock('../../src/api', () => ({ App: { ListTracks: vi.fn(), SearchUpstream: vi.fn(), RefreshSearchCandidates: vi.fn() }, Events: {}, EventsOn: vi.fn() }))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(res => { resolve = res })
  return { promise, resolve }
}
const page = (title: string) => ({ tracks: [{ id: 1, title }], total: 1 }) as Models.app.TrackPage
const upstream = (title: string) => ({ byProvider: [{ rows: [{ title, provider: 'qq', matchRef: title, badges: ['qq'], action: '加入曲库' }], capabilities: ['Lyrics'] }] }) as Models.app.SearchResponse
beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks() })

describe('search refresh ownership', () => {
  it('refreshes the submitted filter without applying unsent text or advanced condition changes', async () => {
    const store = useSearchStore()
    vi.mocked(App.ListTracks).mockResolvedValue(page('old'))
    store.query = 'submitted'
    store.conditions = [{ field: 'artist', value: 'artist' }, { field: 'tag', value: '20' }]
    await store.run()
    store.query = 'draft'
    store.conditions = [{ field: 'artist', value: 'draft artist' }]
    vi.mocked(App.ListTracks).mockResolvedValue(page('new'))
    await store.refreshResults()
    expect(App.ListTracks).toHaveBeenLastCalledWith({ query: 'submitted', artist: 'artist', tagId: 20 })
    expect(store.libraryRows[0].title).toBe('new')
    expect(store.query).toBe('draft')
    expect(store.conditions[0].value).toBe('draft artist')
  })

  it('refreshes every candidate locally, without outbound search, including after failure', async () => {
    const store = useSearchStore()
    store.setScope('qq')
    vi.mocked(App.SearchUpstream).mockResolvedValue(upstream('candidate'))
    await store.run()
    vi.mocked(App.RefreshSearchCandidates).mockRejectedValueOnce(new Error('db unavailable'))
    await store.refreshResults()
    expect(store.upstreamRows[0].title).toBe('candidate')
    vi.mocked(App.RefreshSearchCandidates).mockResolvedValue([{ ...store.upstreamRows[0], localTrackId: 42, action: '已在库中', badges: ['本地', 'qq'] }])
    await store.refreshResults()
    expect(store.upstreamRows[0].localTrackId).toBe(42)
    expect(App.SearchUpstream).toHaveBeenCalledTimes(1)
  })

  it('a local refresh cannot overwrite a newer search or a different scope', async () => {
    const store = useSearchStore()
    vi.mocked(App.ListTracks).mockResolvedValue(page('original'))
    await store.run()
    const old = deferred<Models.app.TrackPage>()
    vi.mocked(App.ListTracks).mockReturnValueOnce(old.promise).mockResolvedValueOnce(page('latest'))
    const refreshing = store.refreshResults(); await Promise.resolve()
    await store.run()
    old.resolve(page('stale')); await refreshing
    expect(store.libraryRows[0].title).toBe('latest')
    const pending = deferred<Models.app.SearchResponse>()
    store.setScope('qq')
    vi.mocked(App.SearchUpstream).mockReturnValueOnce(pending.promise)
    const searching = store.run()
    store.setScope('library')
    pending.resolve(upstream('stale')); await searching
    expect(store.upstreamRows).toEqual([])
    expect(store.searched).toBe(false)
    expect(store.loading).toBe(false)
  })

  it('a library event during an upstream request refreshes local matches when it finishes', async () => {
    const store = useSearchStore()
    store.setScope('qq')
    const pending = deferred<Models.app.SearchResponse>()
    vi.mocked(App.SearchUpstream).mockReturnValueOnce(pending.promise)
    const searching = store.run()
    await store.refreshResults()
    vi.mocked(App.RefreshSearchCandidates).mockResolvedValue([{ title: 'fresh', localTrackId: 7 }] as Models.app.CandidateRow[])
    pending.resolve(upstream('stale')); await searching
    await vi.waitFor(() => expect(store.upstreamRows[0]?.localTrackId).toBe(7))
    expect(App.SearchUpstream).toHaveBeenCalledTimes(1)
  })
})
