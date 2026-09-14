import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, type Models } from '@/api'

/** 当前视图的种类。搜索作用域跟随它。 */
export type ViewKind =
  | 'search'
  | 'all'
  | 'recent'
  | 'artists'
  | 'albums'
  | 'playlist'
  | 'tag'
  | 'artist'
  | 'album'
  | 'importQueue'
  | 'review'

/** 生效的筛选条件，以可清除的 chip 呈现。 */
export interface FilterChip {
  key: string
  label: string
}

export const useLibraryStore = defineStore('library', () => {
  const view = ref<ViewKind>('all')
  /** 当前歌单/标签/艺术家/专辑的标识。 */
  const contextId = ref<number>(0)
  const contextKey = ref<string>('')
  const contextLevel = ref<string>('')
  const contextTitle = ref<string>('全部曲目')

  const playlists = ref<Models.store.Collection[]>([])
  const tags = ref<Models.store.Collection[]>([])
  const tracks = ref<Models.store.TrackView[]>([])
  const total = ref(0)
  const loading = ref(false)
  const error = ref('')

  /** 高级搜索的条件。三个文本字段可任意留空，留空的不参与筛选。 */
  const filter = ref<Models.store.TrackFilter>({} as Models.store.TrackFilter)
  const chips = ref<FilterChip[]>([])

  /** 视图内搜索框的占位文案，明示作用域。 */
  const searchPlaceholder = computed(() => `在“${contextTitle.value}”中搜索…`)

  /** 该视图是否提供右上角的视图内搜索框——只有曲目列表类视图有。 */
  const hasViewSearch = computed(() =>
    ['all', 'recent', 'playlist', 'tag', 'artist'].includes(view.value),
  )

  /** 当前视图对应的基础筛选条件。 */
  function baseFilter(): Models.store.TrackFilter {
    const f = { ...filter.value } as Models.store.TrackFilter
    switch (view.value) {
      case 'playlist':
      case 'tag':
        f.collectionId = contextId.value
        break
      case 'artist':
        f.artistId = contextId.value
        break
      case 'recent':
        f.recentOnly = true
        break
      case 'album':
        if (contextLevel.value === 'release_group') f.releaseGroupRef = contextKey.value
        else if (contextLevel.value === 'release') f.releaseRef = contextKey.value
        else f.derivedAlbumKey = contextKey.value
        break
    }
    return f
  }

  async function loadTracks() {
    loading.value = true
    error.value = ''
    try {
      const page = await App.ListTracks(baseFilter())
      tracks.value = page.tracks ?? []
      total.value = page.total
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function loadCollections() {
    const [p, t] = await Promise.all([App.ListCollections('playlist'), App.ListCollections('tag')])
    playlists.value = p ?? []
    tags.value = t ?? []
  }

  /** 切换视图。切换时清空筛选，避免上一视图的条件泄漏过来。 */
  async function goTo(kind: ViewKind, opts?: { id?: number; key?: string; level?: string; title?: string }) {
    view.value = kind
    contextId.value = opts?.id ?? 0
    contextKey.value = opts?.key ?? ''
    contextLevel.value = opts?.level ?? ''
    contextTitle.value = opts?.title ?? defaultTitle(kind)
    clearFilter()
    if (needsTracks(kind)) await loadTracks()
  }

  function defaultTitle(kind: ViewKind): string {
    switch (kind) {
      case 'all': return '全部曲目'
      case 'recent': return '最近添加'
      case 'artists': return '艺术家'
      case 'albums': return '专辑'
      case 'search': return '搜索'
      case 'importQueue': return '导入队列'
      case 'review': return '复核队列'
      default: return ''
    }
  }

  function needsTracks(kind: ViewKind): boolean {
    return ['all', 'recent', 'playlist', 'tag', 'artist', 'album'].includes(kind)
  }

  /** 就地筛选，不跳页。 */
  async function applyFilter(next: Partial<Models.store.TrackFilter>, newChips: FilterChip[]) {
    filter.value = { ...filter.value, ...next } as Models.store.TrackFilter
    chips.value = newChips
    await loadTracks()
  }

  /** 快速搜索：走 FTS，作用域跟随当前视图。 */
  async function quickSearch(query: string) {
    filter.value = { ...filter.value, query } as Models.store.TrackFilter
    chips.value = query ? [{ key: 'query', label: `搜索="${query}"` }] : []
    await loadTracks()
  }

  async function removeChip(key: string) {
    const next = { ...filter.value }
    // 年份是区间条件，一个 chip 对应两个字段
    if (key === 'year') {
      next.yearFrom = undefined
      next.yearTo = undefined
    } else {
      delete next[key as keyof Models.store.TrackFilter]
    }
    filter.value = next
    chips.value = chips.value.filter((c) => c.key !== key)
    await loadTracks()
  }

  function clearFilter() {
    filter.value = {} as Models.store.TrackFilter
    chips.value = []
  }

  async function clearAllFilters() {
    clearFilter()
    await loadTracks()
  }

  return {
    view, contextId, contextKey, contextLevel, contextTitle,
    playlists, tags, tracks, total, loading, error,
    filter, chips, searchPlaceholder, hasViewSearch,
    baseFilter, loadTracks, loadCollections, goTo,
    applyFilter, quickSearch, removeChip, clearFilter, clearAllFilters,
  }
})
