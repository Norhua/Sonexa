import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'

type SourceResult = Omit<Models.app.CoverSearchEvent, 'convertValues'>

export const useCoverSearchStore = defineStore('coverSearch', () => {
  const target = ref<Models.app.TrackDetail | null>(null)
  const title = ref(''), artist = ref(''), album = ref(''), country = ref('')
  const sources = ref(['caa', 'itunes', 'qq', '163'])
  const results = ref<Record<string, SourceResult>>({})
  const error = ref('')
  const revision = ref(0)
  const ready = ref(false)
  // 每个来源拥有独立后端会话，重试一个来源不撤销其他卡片的采用授权。
  const sessions = new Map<string, string>()
  let generation = 0
  let off: (() => void) | null = null
  const cards = computed(() => Object.values(results.value).flatMap(result => result.cards ?? []))

  function receive(event: SourceResult) {
    if (event.generation !== generation || event.searchId !== sessions.get(event.source) || event.releaseId !== target.value?.track.releaseId) return
    results.value = { ...results.value, [event.source]: event }
  }
  async function cancel(ids: string[]) {
    await Promise.all(ids.map(id => App.CancelCoverSearch(id)))
  }
  async function open(detail: Models.app.TrackDetail) {
    const closing = close()
    const current = generation
    target.value = detail
    title.value = detail.track.title
    artist.value = detail.track.artist ?? ''
    album.value = detail.track.album ?? ''
    off = EventsOn(Events.CoverResults, receive)
    try {
      await closing
      const value = await App.CoverRevision(detail.track.releaseId ?? 0)
      if (current !== generation) return
      revision.value = value
      ready.value = true
    } catch (e) { if (current === generation) error.value = String(e) }
  }
  async function search(only?: string) {
    if (!ready.value || !target.value?.track.releaseId) return
    const current = generation
    const chosen = only ? [only] : [...sources.value]
    if (!chosen.length) { error.value = '请选择封面来源'; return }
    const previous = only ? [sessions.get(only)].filter((id): id is string => !!id) : [...sessions.values()]
    if (only) sessions.delete(only)
    else { sessions.clear(); results.value = {} }
    const request = { generation: current, releaseId: target.value.track.releaseId, title: title.value, artist: artist.value, album: album.value, country: country.value }
    error.value = ''
    const jobs = chosen.map(source => {
      const id = crypto.randomUUID()
      sessions.set(source, id)
      results.value[source] = { generation: current, searchId: id, releaseId: request.releaseId, source, state: 'loading', cards: [], error: '' }
      return { source, id }
    })
    try { await cancel(previous) }
    catch (e) { if (current === generation) error.value = String(e) }
    await Promise.all(jobs.map(async ({ source, id }) => {
      if (current !== generation || sessions.get(source) !== id) return
      try {
        await App.StartCoverSearch({ ...request, searchId: id, sources: [source] })
        // 关闭可先于启动绑定完成：迟到的启动必须再次取消，避免遗留后台会话。
        if (current !== generation || sessions.get(source) !== id) {
          await App.CancelCoverSearch(id)
          return
        }
        // 同时覆盖绑定返回之前已发出的事件；订阅不因等待绑定而出现空窗。
        const snapshot = await App.CoverSearchResults(id)
        if (current === generation) snapshot.forEach(receive)
      } catch (e) {
        if (current !== generation || sessions.get(source) !== id) return
        receive({ generation: current, searchId: id, releaseId: request.releaseId, source, state: 'error', cards: [], error: String(e) })
      }
    }))
  }
  async function adopt(variantId: number) {
    const owner = Object.values(results.value).find(result => result.cards?.some(card => card.image.variants.some(v => v.id === variantId)))
    if (!owner || sessions.get(owner.source) !== owner.searchId) throw new Error('所选图片已不属于当前搜索')
    const current = generation
    const result = await App.AdoptCoverCandidate(owner.searchId, variantId, revision.value)
    if (current !== generation) return null
    revision.value = result.revision
    return result
  }
  async function close() {
    generation++
    const previous = [...sessions.values()]
    sessions.clear()
    off?.(); off = null
    target.value = null
    ready.value = false
    results.value = {}
    error.value = ''
    await cancel(previous)
  }
  return { target, title, artist, album, country, sources, results, cards, error, ready, open, search, adopt, close }
})
