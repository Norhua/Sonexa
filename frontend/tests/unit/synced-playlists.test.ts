import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, EventsOn, type Models } from '../../src/api'
import { useSyncedPlaylistsStore } from '../../src/stores/syncedPlaylists'
import { useLibraryStore } from '../../src/stores/library'
import { listRows, uniqueTracks } from '../../src/lib/trackRows'
vi.mock('../../src/api', () => ({
  App: { ListSyncedPlaylists: vi.fn(), GetSyncedPlaylistEntries: vi.fn(), ListTracks: vi.fn() },
  Events: { SyncedPlaylistsChanged: 'synced-playlists:changed' }, EventsOn: vi.fn(),
}))
const binding = (id: number) => ({ id, name: `歌单 ${id}`, importedCount: 2 }) as Models.app.SyncedPlaylistView
const rows = (id: number) => [{ entryId: id, trackId: 8, upstreamPresent: false, inLibrary: false, track: { id: 8, title: 'Shared' } }] as Models.store.SyncedTrackRow[]
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks()
  vi.mocked(App.ListSyncedPlaylists).mockResolvedValue([binding(1), binding(2)])
  vi.mocked(App.GetSyncedPlaylistEntries).mockImplementation(async id => rows(id))
  vi.mocked(App.ListTracks).mockResolvedValue({ tracks: [], total: 0 })
})
it('keeps late responses out of a new context and retains data on refresh failure', async () => {
  let resolve!: (rows: Models.store.SyncedTrackRow[]) => void
  vi.mocked(App.GetSyncedPlaylistEntries).mockReturnValueOnce(new Promise(r => { resolve = r }))
  const store = useSyncedPlaylistsStore()
  const a = store.activate(1)
  // Let the first request actually start; same-tick requests deliberately coalesce.
  await Promise.resolve()
  const b = store.activate(2)
  resolve(rows(1)); await Promise.all([a, b])
  expect(store.entries[0]?.entryId).toBe(2)
  vi.mocked(App.GetSyncedPlaylistEntries).mockRejectedValueOnce(new Error('offline'))
  await store.refresh()
  expect(store.entries[0]?.entryId).toBe(2)
  expect(store.error).toContain('offline')
})
it('subscribes once and exits a deleted navigation context', async () => {
  let callback!: () => void
  const off = vi.fn()
  vi.mocked(EventsOn).mockImplementation((_event, fn) => { callback = fn; return off })
  const store = useSyncedPlaylistsStore(), library = useLibraryStore()
  store.subscribe(); store.subscribe()
  expect(EventsOn).toHaveBeenCalledTimes(1)
  await library.goTo('syncedPlaylist', { id: 1 })
  expect(App.ListTracks).not.toHaveBeenCalled()
  await store.activate(1)
  vi.mocked(App.ListSyncedPlaylists).mockResolvedValue([])
  callback(); await store.refresh()
  expect(library.view).toBe('all')
  store.dispose(); expect(off).toHaveBeenCalledOnce()
})
it('preserves repeated entry keys and selects upstream-missing rows while deduplicating batch IDs', () => {
  const occurrences = [1, 2].map(entry => ({ id: 8, title: 'Shared', rowKey: `synced:${entry}`, upstreamMissing: entry === 2 }))
  expect(listRows(occurrences).map(row => row.key)).toEqual(['synced:1', 'synced:2'])
  expect(listRows(occurrences).every(row => !row.ghost)).toBe(true)
  expect(uniqueTracks(occurrences).map(row => row.id)).toEqual([8])
})
