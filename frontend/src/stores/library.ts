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
  /** 专辑视图：非零时当前专辑代表一个发行组，曲目取整组的。 */
  const contextGroupId = ref<number>(0)
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
        // 歌集升格为实体后专辑就是一条 release；有发行组时取整组的曲目，
        // 因为列表里同组的多个版本合为一条。
        if (contextGroupId.value) f.releaseGroupId = contextGroupId.value
        else f.releaseId = contextId.value
        break
    }
    return f
  }

  let trackRequest = 0
  async function loadTracks() {
    const request = ++trackRequest
    loading.value = true
    error.value = ''
    try {
      const page = await App.ListTracks(baseFilter())
      if (request !== trackRequest) return
      tracks.value = page.tracks ?? []
      total.value = page.total
    } catch (e) {
      if (request === trackRequest) error.value = String(e)
    } finally {
      if (request === trackRequest) loading.value = false
    }
  }

  async function loadCollections() {
    const [p, t] = await Promise.all([App.ListCollections('playlist'), App.ListCollections('tag')])
    playlists.value = p ?? []
    tags.value = t ?? []
    const activeTag = filter.value.tagId
    if (activeTag) {
      // 删除/重命名标签后同步筛选说明；调用方随后重载当前曲目。
      if (!tags.value.some(tag => tag.id === activeTag)) {
        filter.value.tagId = 0
        chips.value = chips.value.filter(chip => chip.key !== 'tagId')
      } else {
        chips.value = chips.value.map(chip => chip.key === 'tagId' ? tagChip(activeTag)[0] : chip)
      }
    }
  }

  /** 切换视图清空筛选；同一三栏浏览页内换艺术家/专辑时保留标签条件。 */
  async function goTo(
    kind: ViewKind,
    opts?: { id?: number; groupId?: number; title?: string },
  ) {
    // 导航本身即使不读取曲目也使旧请求失效；旧响应不能清掉新请求的 loading。
    trackRequest++
    loading.value = false
    error.value = ''
    if (view.value !== kind || contextId.value !== (opts?.id ?? 0) || contextGroupId.value !== (opts?.groupId ?? 0)) {
      tracks.value = []
      total.value = 0
    }
    const browseFamily = (value: ViewKind) => value === 'artist' ? 'artists' : value === 'album' ? 'albums' : value
    const family = browseFamily(kind)
    const tagId = ['artists', 'albums'].includes(family) && browseFamily(view.value) === family ? filter.value.tagId : 0
    view.value = kind
    contextId.value = opts?.id ?? 0
    contextGroupId.value = opts?.groupId ?? 0
    contextTitle.value = opts?.title ?? defaultTitle(kind)
    clearFilter()
    if (tagId) {
      filter.value.tagId = tagId
      chips.value = tagChip(tagId)
    }
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
    const keys = new Set(Object.keys(next))
    chips.value = [...chips.value.filter(chip => !keys.has(chip.key)), ...newChips]
    await loadTracks()
  }

  /** 快速搜索：走 FTS，作用域跟随当前视图。 */
  async function quickSearch(query: string) {
    filter.value = { ...filter.value, query } as Models.store.TrackFilter
    chips.value = [...chips.value.filter(chip => chip.key !== 'query'), ...(query ? [{ key: 'query', label: `搜索="${query}"` }] : [])]
    await loadTracks()
  }

  function tagChip(id: number): FilterChip[] {
    return id ? [{ key: 'tagId', label: `标签=${tags.value.find(tag => tag.id === id)?.name ?? id}` }] : []
  }

  async function pickTag(id: number) {
    await applyFilter({ tagId: id }, tagChip(id))
  }

  async function removeChip(key: string) {
    const next = { ...filter.value }
    delete next[key as keyof Models.store.TrackFilter]
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
    view, contextId, contextGroupId, contextTitle,
    playlists, tags, tracks, total, loading, error,
    filter, chips, searchPlaceholder, hasViewSearch,
    baseFilter, loadTracks, loadCollections, goTo,
    applyFilter, quickSearch, pickTag, removeChip, clearFilter, clearAllFilters,
  }
})
