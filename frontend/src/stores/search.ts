import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, type Models } from '@/api'

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

  /** 切换范围时清掉上一次的结果：两种范围的结果结构不同，混着显示会误导。 */
  function setScope(next: string) {
    if (scope.value === next) return
    scope.value = next
    libraryRows.value = []
    upstreamRows.value = []
    searched.value = false
    error.value = ''
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
    libraryRows.value = []
    upstreamRows.value = []
    searched.value = false
  }

  /** 主搜索框的文本在没有显式「曲名」条件时充当曲名。 */
  function effectiveTitle(): string {
    return valueOf('title').trim() || query.value.trim()
  }

  async function run() {
    loading.value = true
    error.value = ''
    try {
      if (isLibrary.value) await runLibrary()
      else await runUpstream()
      searched.value = true
    } catch (e) {
      error.value = String(e)
    } finally {
      loading.value = false
    }
  }

  async function runLibrary() {
    const filter: Partial<Models.store.TrackFilter> = {}
    // 主搜索框走 FTS，覆盖曲名/艺术家/专辑/署名/关联艺术家
    if (query.value.trim()) filter.query = query.value.trim()
    for (const c of conditions.value) {
      const v = c.value.trim()
      if (!v) continue
      if (c.field === 'tag') filter.tagId = Number(v)
      else filter[c.field] = v
    }
    const page = await App.ListTracks(filter as Models.store.TrackFilter)
    libraryRows.value = page.tracks ?? []
  }

  async function runUpstream() {
    const res = await App.SearchUpstream(
      effectiveTitle(),
      valueOf('artist').trim(),
      valueOf('album').trim(),
      [scope.value],
      country.value,
    )
    const provider = res?.byProvider?.[0]
    if (provider?.error) throw new Error(provider.error)
    upstreamRows.value = provider?.rows ?? []
    upstreamCaps.value = provider?.capabilities ?? []

    // 一页列表只发起一次批量预取，并发度由 Go 的池决定
    if (upstreamRows.value.length > 0) {
      const refs = await App.BuildCoverRefs(upstreamRows.value)
      if (refs?.length) void App.PrefetchCovers(refs)
    }
  }

  return {
    scope,
    query,
    conditions,
    libraryRows,
    upstreamRows,
    upstreamCaps,
    loading,
    error,
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
    run,
  }
})
