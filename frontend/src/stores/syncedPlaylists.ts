import { defineStore } from 'pinia'
import { ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'
import { createRefreshQueue } from '@/lib/refreshQueue'
import { useLibraryStore } from './library'

export const useSyncedPlaylistsStore = defineStore('syncedPlaylists', () => {
  const playlists = ref<Models.app.SyncedPlaylistView[]>([])
  const entries = ref<Models.store.SyncedTrackRow[]>([])
  const activeId = ref(0), query = ref(''), loading = ref(false), error = ref('')
  let off: (() => void) | null = null
  let generation = 0
  function makeQueue() {
    return createRefreshQueue(async current => {
      const id = activeId.value, text = query.value, version = generation
      loading.value = true
      try {
        const [list, rows] = await Promise.all([
          App.ListSyncedPlaylists(),
          id ? App.GetSyncedPlaylistEntries(id, text) : Promise.resolve([]),
        ])
        if (!current() || version !== generation) return
        playlists.value = list ?? []
        entries.value = rows ?? []
        error.value = ''
        const library = useLibraryStore()
        if (library.view === 'syncedPlaylist') {
          const active = playlists.value.find(p => p.id === library.contextId)
          if (!active) await library.goTo('all')
          else library.contextTitle = active.name
        }
      } catch (e) {
        if (current() && version === generation) {
          error.value = String(e)
          // A deleted active binding makes the entry read fail. Read navigation
          // separately, while keeping the old rows on other transient failures.
          try {
            const list = await App.ListSyncedPlaylists()
            if (!current() || version !== generation) return
            playlists.value = list ?? []
            const library = useLibraryStore()
            if (library.view === 'syncedPlaylist' && !playlists.value.some(p => p.id === library.contextId)) await library.goTo('all')
          } catch { /* Retain the last confirmed list. */ }
        }
      } finally { if (version === generation) loading.value = false }
    })
  }
  let queue = makeQueue()
  function refresh() { return queue.request() }
  function activate(id: number, text = '') {
    if (activeId.value !== id) entries.value = []
    activeId.value = id; query.value = text; generation++
    return refresh()
  }
  function subscribe() { if (!off) off = EventsOn(Events.SyncedPlaylistsChanged, () => { void refresh() }) }
  function dispose() { off?.(); off = null; generation++; queue.dispose(); queue = makeQueue(); loading.value = false }
  return { playlists, entries, activeId, query, loading, error, refresh, activate, subscribe, dispose }
})
