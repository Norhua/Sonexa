import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, type Models } from '@/api'
import { categoryLabels, defaultLibraryOnly, supportsLibraryScope, supportsSyncedCategory, toggleCategory, type FilterCategory } from '@/lib/viewFilters'

/** 当前视图的种类。搜索作用域跟随它。 */
export type ViewKind =
  | 'search'
  | 'all'
  | 'recent'
  | 'artists'
  | 'albums'
  | 'syncedPlaylist'
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
  const libraryOnly = ref(true)
  const categoryFilter = computed(() => ({
    includeSynced: supportsLibraryScope(view.value) && !libraryOnly.value,
    playlistIds: filter.value.playlistIds ?? null,
    tagIds: filter.value.tagIds ?? null,
    syncedPlaylistIds: supportsSyncedCategory(view.value) ? filter.value.syncedPlaylistIds ?? null : null,
    tagId: filter.value.tagId,
  } as Models.store.TrackFilter))
  const hasCategoryFilter = computed(() => !!filter.value.tagId ||
    Object.keys(categoryLabels).some(key => filter.value[key as FilterCategory] != null))
  const allSongsSelected = computed(() => !hasCategoryFilter.value &&
    (!supportsLibraryScope(view.value) || !libraryOnly.value))

  /** 视图内搜索框的占位文案，明示作用域。 */
  const searchPlaceholder = computed(() => `在“${contextTitle.value}”中搜索…`)

  /** 该视图是否提供右上角的视图内搜索框——只有曲目列表类视图有。 */
  const hasViewSearch = computed(() =>
    ['all', 'recent', 'playlist', 'tag', 'artist'].includes(view.value),
  )

  /** 当前视图对应的基础筛选条件。 */
  function baseFilter(): Models.store.TrackFilter {
    const f = { ...filter.value, ...categoryFilter.value } as Models.store.TrackFilter
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

  let collectionRequest = 0
  async function loadCollections() {
    const request = ++collectionRequest
    const [p, t] = await Promise.all([App.ListCollections('playlist'), App.ListCollections('tag')])
    if (request !== collectionRequest) return
    playlists.value = p ?? []
    tags.value = t ?? []
    refreshCategoryOptions('playlistIds', playlists.value)
    refreshCategoryOptions('tagIds', tags.value)
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

  function refreshCategoryOptions(key: FilterCategory, options: { id: number; name: string }[]) {
    const selected = filter.value[key]
    if (selected == null) return
    const remaining = options.filter(item => selected.includes(item.id))
    const ids = remaining.map(item => item.id)
    if (ids.length !== selected.length) filter.value = { ...filter.value, [key]: ids } as Models.store.TrackFilter
    const label = categoryChipLabel(key, ids, remaining.map(item => item.name))
    chips.value = chips.value.map(chip => chip.key === key ? { key, label } : chip)
  }

  /** 切换页面重置；同一艺术家/专辑浏览内保留范围及分类多选。 */
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
    const preserve = ['artists', 'albums'].includes(family) && browseFamily(view.value) === family
    const previous = { ...categoryFilter.value }
    const previousLibraryOnly = libraryOnly.value
    const previousChips = chips.value.filter(chip => ['tagId', ...Object.keys(categoryLabels)].includes(chip.key))
    view.value = kind
    contextId.value = opts?.id ?? 0
    contextGroupId.value = opts?.groupId ?? 0
    contextTitle.value = opts?.title ?? defaultTitle(kind)
    clearFilter()
    if (preserve) {
      filter.value = { ...filter.value, ...previous } as Models.store.TrackFilter
      libraryOnly.value = previousLibraryOnly
      chips.value = previousChips
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
    await reloadFilteredTracks()
  }

  async function reloadFilteredTracks() {
    if (needsTracks(view.value)) await loadTracks()
  }

  async function selectAllSongs() {
    libraryOnly.value = !supportsLibraryScope(view.value)
    await applyFilter({ playlistIds: undefined, tagIds: undefined, syncedPlaylistIds: undefined, tagId: 0 }, [])
  }

  async function selectLibraryOnly() {
    libraryOnly.value = true
    await reloadFilteredTracks()
  }

  function categoryChipLabel(key: FilterCategory, ids: number[], names: string[]) {
    const label = ids.length === 0 ? '未选择' : ids.length > 2 ? `已选 ${ids.length} 项` : names.join('、') || `${ids.length} 项`
    return `${categoryLabels[key]}=${label}`
  }

  async function selectCategory(key: FilterCategory, ids: number[] | null, names: string[] = []) {
    await applyFilter({ [key]: ids ?? undefined, ...(key === 'tagIds' ? { tagId: 0 } : {}) },
      ids === null ? [] : [{ key, label: categoryChipLabel(key, ids, names) }])
  }

  async function toggleCategoryOption(key: FilterCategory, id: number, options: { id: number; name: string }[]) {
    const ids = toggleCategory(filter.value[key], id, options.map(item => item.id))
    await selectCategory(key, ids, options.filter(item => ids?.includes(item.id)).map(item => item.name))
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
    await reloadFilteredTracks()
  }

  function clearFilter() {
    filter.value = {} as Models.store.TrackFilter
    libraryOnly.value = defaultLibraryOnly(view.value)
    chips.value = []
  }

  async function clearAllFilters() {
    clearFilter()
    await reloadFilteredTracks()
  }

  return {
    view, contextId, contextGroupId, contextTitle,
    playlists, tags, tracks, total, loading, error,
    filter, chips, searchPlaceholder, hasViewSearch,
    libraryOnly, categoryFilter, hasCategoryFilter, allSongsSelected,
    selectAllSongs, selectLibraryOnly, selectCategory, toggleCategoryOption, refreshCategoryOptions,
    baseFilter, loadTracks, loadCollections, goTo, needsTracks,
    applyFilter, quickSearch, pickTag, removeChip, clearFilter, clearAllFilters,
  }
})
