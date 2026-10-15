import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'
import { createRefreshQueue } from '@/lib/refreshQueue'

/** 搜索范围：资料库，或某一个上游。 */
export const LibraryScope = 'library'

/** 高级搜索支持的条件字段。v1 收敛为这四项。 */
export type SearchField = 'title' | 'artist' | 'album' | 'tag'

/** 一条已添加的高级条件。 */
export interface Condition {
  field: SearchField
  value: string
}

export const FIELD_LABELS: Record<SearchField, string> = {
  title: '曲名',
  artist: '艺术家',
  album: '专辑',
  tag: '标签',
}

/** 上游搜索用不到标签——它筛的是本地收藏的组织方式。 */
export const UPSTREAM_FIELDS: SearchField[] = ['title', 'artist', 'album']

/**
 * 搜索页的状态。
 *
 * 搜索是一个独立页面而不是顶栏上的一个框：库内结果与上游结果的可用动作
 * 完全不同，它们各需一整块结果区，挤在下拉浮层里两边都放不下。
 *
 * 范围为单选：多个上游并发查询时结果会落进同一个列表，来源难分、
 * 排序也不可比（各家的 relevance 不同源）。
 */
export const useSearchStore = defineStore('search', () => {
  /** LibraryScope 或某个 provider 的 id。默认搜资料库。 */
  const scope = ref<string>(LibraryScope)
  /** 主搜索框的文本。 */
  const query = ref('')
  /** 高级条件：从下拉选择后添加，未添加的不参与查询。 */
  const conditions = ref<Condition[]>([])

  const libraryRows = ref<Models.store.TrackView[]>([])
  const upstreamRows = ref<Models.app.CandidateRow[]>([])
  const upstreamCaps = ref<string[]>([])
  const loading = ref(false)
  const error = ref('')
  const refreshError = ref('')
  /** 是否已经搜过一次——用于区分「还没搜」与「搜了但没结果」。 */
  const searched = ref(false)
  const country = ref('')

  const isLibrary = computed(() => scope.value === LibraryScope)

  /** 取某条件的值；未添加时为空串。 */
  function valueOf(field: SearchField): string {
    return conditions.value.find((c) => c.field === field)?.value ?? ''
  }

  function addCondition(field: SearchField) {
    if (conditions.value.some((c) => c.field === field)) return
    conditions.value = [...conditions.value, { field, value: '' }]
  }

  function removeCondition(field: SearchField) {
    conditions.value = conditions.value.filter((c) => c.field !== field)
  }

  function setCondition(field: SearchField, value: string) {
    conditions.value = conditions.value.map((c) => (c.field === field ? { ...c, value } : c))
  }

  function resetConditions() {
    conditions.value = []
  }

  let request = 0
  let libraryRevision = 0
  let submittedFilter: Models.store.TrackFilter | null = null
  let unsubscribe: (() => void) | null = null

  function resetResults() {
    request++
    loading.value = false
    libraryRows.value = []
    upstreamRows.value = []
    upstreamCaps.value = []
    searched.value = false
    submittedFilter = null
    error.value = ''
    refreshError.value = ''
  }

  /** 切换范围时清掉上一次的结果，并使旧搜索与本地刷新失效。 */
  function setScope(next: string) {
    if (scope.value === next) return
    scope.value = next
    resetResults()
    // 标签条件只对资料库有意义，切到上游时移除
    if (next !== LibraryScope) removeCondition('tag')
  }

  /** 从别处带着条件跳进搜索页（如曲目右键「查看上游」）。 */
  function prefillUpstream(providerId: string, fields: { title: string; artist: string; album: string }) {
    scope.value = providerId
    query.value = fields.title
    conditions.value = (['artist', 'album'] as SearchField[])
      .filter((f) => (f === 'artist' ? fields.artist : fields.album).trim() !== '')
      .map((f) => ({ field: f, value: f === 'artist' ? fields.artist : fields.album }))
    resetResults()
  }

  /** 主搜索框的文本在没有显式「曲名」条件时充当曲名。 */
  function effectiveTitle(): string {
    return valueOf('title').trim() || query.value.trim()
  }

  async function run() {
    const token = ++request
    const revision = libraryRevision
    const local = isLibrary.value
    const filter = libraryFilter()
    loading.value = true
    error.value = ''
    try {
      if (local) {
        const page = await App.ListTracks(filter)
        if (token !== request) return
        libraryRows.value = page.tracks ?? []
        submittedFilter = filter
      } else {
        const res = await App.SearchUpstream(
          effectiveTitle(), valueOf('artist').trim(), valueOf('album').trim(), [scope.value], country.value,
        )
        if (token !== request) return
        const provider = res?.byProvider?.[0]
        if (provider?.error) throw new Error(provider.error)
        upstreamRows.value = provider?.rows ?? []
        upstreamCaps.value = provider?.capabilities ?? []
      }
      searched.value = true
      refreshError.value = ''
    } catch (e) {
      if (token === request) error.value = String(e)
    } finally {
      if (token === request) {
        loading.value = false
        // 搜索在途的库变化不能丢失；只本地重读，不重复出网查询。
        if (revision !== libraryRevision) void refreshResults()
      }
    }
  }

  function libraryFilter(): Models.store.TrackFilter {
    const filter: Partial<Models.store.TrackFilter> = {}
    if (query.value.trim()) filter.query = query.value.trim()
    for (const c of conditions.value) {
      const v = c.value.trim()
      if (!v) continue
      if (c.field === 'tag') filter.tagId = Number(v)
      else filter[c.field] = v
    }
    return filter as Models.store.TrackFilter
  }

  function makeRefreshQueue() {
    return createRefreshQueue(async current => {
      if (!searched.value || loading.value) return
      const token = request
      try {
        if (isLibrary.value && submittedFilter) {
          const page = await App.ListTracks({ ...submittedFilter })
          if (current() && token === request) libraryRows.value = page.tracks ?? []
        } else if (!isLibrary.value && upstreamRows.value.length) {
          const rows = await App.RefreshSearchCandidates(upstreamRows.value)
          if (current() && token === request) upstreamRows.value = rows ?? []
        }
        if (current() && token === request) refreshError.value = ''
      } catch (e) {
        if (current() && token === request) refreshError.value = String(e)
      }
    })
  }
  let refreshQueue = makeRefreshQueue()
  function refreshResults() {
    libraryRevision++
    return refreshQueue.request()
  }
  function subscribe() {
    if (!unsubscribe) unsubscribe = EventsOn(Events.LibraryChanged, () => { void refreshResults() })
  }
  function unsubscribeAll() {
    unsubscribe?.()
    unsubscribe = null
    request++
    refreshQueue.dispose()
    refreshQueue = makeRefreshQueue()
  }

  return {
    scope,
    query,
    conditions,
    libraryRows,
    upstreamRows,
    upstreamCaps,
    loading,
    error, refreshError,
    searched,
    country,
    isLibrary,
    valueOf,
    addCondition,
    removeCondition,
    setCondition,
    resetConditions,
    setScope,
    prefillUpstream,
    run, refreshResults, subscribe, unsubscribeAll,
  }
})
