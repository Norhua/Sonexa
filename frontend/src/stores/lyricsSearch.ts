import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, Events, EventsOn, type Models } from '@/api'
import { useSourceSelection } from '@/lib/sourceSelection'
import { compareLyrics } from '@/lib/lyricsRanking'
import { lyricRoundFinished, pendingLyricCard, type LyricCard, type LyricDocument, type LyricProgress } from '@/lib/lyricsPresentation'
export { lyricFormat } from '@/lib/lyricsPresentation'

type Result = { state: 'loading' | 'ok' | 'error'; phase: string; sequence: number; settled: boolean; cards: LyricCard[]; error: string; progress?: LyricProgress }

export const useLyricsSearchStore = defineStore('lyricsSearch', () => {
  const target = ref<Models.app.TrackDetail | null>(null)
  const title = ref(''), artist = ref(''), album = ref('')
  const sourceSelection = useSourceSelection<Models.lyrics.Source>()
  const { selected: sources, options: sourceOptions } = sourceSelection
  const results = ref<Record<string, Result>>({})
  const documents = ref<Record<string, LyricDocument>>({})
  const versions = ref<Record<string, number>>({}), approved = ref<Record<string, number>>({})
  const failures = ref<Record<string, string>>({}), loading = ref<Record<string, boolean>>({})
  const selected = ref<string | null>(null), error = ref(''), notice = ref(''), ready = ref(false), busy = ref(false)
  const cards = computed(() => Object.values(results.value).flatMap(r => r.cards).sort(compareLyrics))
  const selection = computed(() => cards.value.find(c => c.id === selected.value))
  const document = computed(() => selected.value ? documents.value[selected.value] : undefined)
  const documentVersion = computed(() => selected.value ? versions.value[selected.value] ?? 0 : 0)
  const canAdopt = computed(() => !!selected.value && !!document.value && !loading.value[selected.value] && !busy.value && approved.value[selected.value] === documentVersion.value && documentVersion.value > 0)
  const sessions = new Map<string, string>(), jobs = new Map<string, number>(), fetchJobs = new Map<string, number>()
  let generation = 0, revision = 0, job = 0, selectionJob = 0, manualSelection = false
  let submitted: Models.lyrics.Query | undefined
  let off: (() => void) | undefined

  function receive(event: LyricProgress) {
    const old = results.value[event.source]
    if (!target.value || sessions.get(event.source) !== event.sessionId || !old || event.sequence <= old.sequence) return
    const next = event.candidates.map(({ candidate, ...summary }) => {
      const card = { ...candidate, ...summary }
      const version = versions.value[card.id] ?? 0
      // Fetch may resolve before its summary event. Never regress that version;
      // loading/zero is an explicit refresh and invalidates approval immediately.
      if (card.documentVersion < version && card.fetchState !== 'loading') return old.cards.find(c => c.id === card.id) ?? card
      if (card.documentVersion > version || card.fetchState === 'loading') {
        delete documents.value[card.id]; delete approved.value[card.id]
      }
      return card
    })
    results.value[event.source] = { ...old, phase: event.phase, sequence: event.sequence, state: event.phase === 'error' ? 'error' : 'ok', cards: next, progress: event }
    const chosen = next.find(c => c.id === selected.value)
    if (chosen?.prefetch && ['queued', 'loading', 'ready'].includes(chosen.fetchState) && old.cards.find(c => c.id === chosen.id)?.fetchState !== chosen.fetchState) void fetchSelected()
    void maybeAutoPreview()
  }
  async function maybeAutoPreview() {
    const all = Object.values(results.value)
    if (manualSelection || selected.value || !all.length || all.some(r => !r.settled || !lyricRoundFinished(r.phase))) return
    const best = cards.value.find(c => c.fetchState === 'ready' && c.totalScore != null && c.highConfidence && c.autoPreviewEligible)
    if (best) await select(best.id, false)
  }
  async function cancel(ids: string[]) {
    await Promise.allSettled(ids.map(id => App.CancelLyricsSearch(id)))
  }
  function clearCard(id: string) {
    delete documents.value[id]; delete versions.value[id]; delete approved.value[id]; delete failures.value[id]; delete loading.value[id]
    fetchJobs.delete(id)
    if (selected.value === id) { selected.value = null; selectionJob++ }
  }
  async function close() {
    generation++; selectionJob++
    off?.(); off = undefined
    const ids = [...sessions.values()]; sessions.clear(); jobs.clear(); fetchJobs.clear()
    target.value = null; ready.value = false; sourceOptions.value = []; results.value = {}; documents.value = {}; versions.value = {}; approved.value = {}; failures.value = {}; loading.value = {}
    selected.value = null; error.value = ''; notice.value = ''; busy.value = false; submitted = undefined; manualSelection = false
    await cancel(ids)
  }
  async function open(detail: Models.app.TrackDetail) {
    const closing = close(), current = generation
    target.value = detail; title.value = detail.track.title; artist.value = detail.track.artist ?? ''; album.value = detail.track.album ?? ''
    off = EventsOn(Events.LyricsProgress, (event: LyricProgress) => { if (current === generation) receive(event) })
    try {
      await closing
      const [rev, list] = await Promise.all([App.LyricsRevision(detail.track.id), App.ListLyricsSources()])
      if (current !== generation) return
      sourceSelection.update(list)
      revision = rev; ready.value = true
    } catch (e) { if (current === generation) error.value = String(e) }
  }
  async function search(only?: string) {
    if (!ready.value || !target.value || busy.value) return
    let chosen: string[]
    try { chosen = sourceSelection.choose(only) }
    catch (e) { error.value = String(e); return }
    if (!chosen.length) { error.value = '请选择歌词来源'; return }
    const query = only && submitted ? { ...submitted } : { title: title.value, artist: artist.value, album: album.value, durationMs: target.value.track.durationMs ?? target.value.metadata?.durationMs ?? 0 }
    if (![query.title, query.artist, query.album].some(s => s.trim())) { error.value = '请输入歌词搜索条件'; return }
    submitted = { ...query }
    const current = generation, trackID = target.value.track.id
    error.value = ''; notice.value = ''
    const previous = only ? [sessions.get(only)].filter((v): v is string => !!v) : [...sessions.values()]
    if (!only) {
      sessions.clear(); jobs.clear()
      for (const c of cards.value) clearCard(c.id)
      results.value = {}; selected.value = null; manualSelection = false
    } else {
      sessions.delete(only)
      for (const c of results.value[only]?.cards ?? []) clearCard(c.id)
    }
    const work = chosen.map(source => {
      const token = ++job; jobs.set(source, token)
      results.value[source] = { state: 'loading', phase: 'searching', sequence: 0, settled: false, cards: [], error: '' }
      return { source, token }
    })
    await cancel(previous)
    await Promise.all(work.map(async ({ source, token }) => {
      const valid = () => current === generation && jobs.get(source) === token
      if (!valid()) return
      let id: string | undefined
      try {
        id = await App.CreateLyricsSearch(trackID)
        if (!valid()) { await cancel([id]); return }
        sessions.set(source, id)
        const found = await App.SearchLyrics(id, source, query)
        if (!valid()) return
        // An event can precede the search response. Keep its newer summaries.
        if (results.value[source]!.sequence === 0) {
          results.value[source] = { state: 'ok', phase: 'ready', sequence: 0, settled: false, cards: found.map((c, i) => pendingLyricCard(c, i < 3)), error: '' }
        }
        const started = await App.StartLyricsPrefetch(id)
        if (!valid()) return
        receive(started)
        // Exactly one snapshot closes the subscription/initial-call gap.
        const snapshot = await App.LyricsSearchSnapshot(id)
        if (!valid()) return
        receive(snapshot)
        results.value[source]!.settled = true
      } catch (e) {
        if (!valid()) return
        results.value[source] = { ...results.value[source]!, state: 'error', phase: 'error', settled: true, error: String(e) }
        if (id) { sessions.delete(source); await cancel([id]) }
      }
    }))
    if (current === generation) await maybeAutoPreview()
  }
  async function select(id: string, manual = true) {
    const card = cards.value.find(c => c.id === id)
    if (!card || busy.value) return
    if (manual) manualSelection = true
    if (selected.value !== id) { selected.value = id; selectionJob++ }
    // Non-prefetched/failed items need the explicitly labelled fetch button.
    if (card.fetchState === 'ready' || card.fetchState === 'queued' || card.fetchState === 'loading') await fetchSelected()
  }
  async function fetchSelected(refresh = false) {
    const card = selection.value
    if (!card || busy.value || loading.value[card.id]) return
    const id = card.id, session = sessions.get(card.source), current = generation
    if (!session || (!refresh && documents.value[id])) return
    const phase = results.value[card.source]?.phase ?? ''
    if (!lyricRoundFinished(phase) && !['queued', 'loading'].includes(card.fetchState)) return
    const token = ++job; fetchJobs.set(id, token)
    loading.value[id] = true; delete failures.value[id]; delete approved.value[id]
    if (refresh) delete documents.value[id]
    const valid = () => current === generation && sessions.get(card.source) === session && fetchJobs.get(id) === token
    try {
      const preview = await (refresh ? App.RefreshLyrics(session, id) : App.FetchLyrics(session, id))
      if (!valid()) return
      const latest = cards.value.find(c => c.id === id)
      if (!preview.document?.text || !preview.document.rows?.length || preview.version <= 0) throw new Error('歌词文档不完整')
      if (latest && latest.documentVersion > preview.version) return
      documents.value[id] = preview.document; versions.value[id] = preview.version
    } catch (e) { if (valid()) failures.value[id] = String(e) }
    finally { if (valid()) loading.value[id] = false }
  }
  // Called by the mounted preview only after Vue has rendered the document.
  async function acknowledge(id: string, version: number) {
    const card = selection.value, current = generation, token = selectionJob
    if (!card || card.id !== id || !document.value || loading.value[id] || versions.value[id] !== version) return
    const session = sessions.get(card.source)
    if (!session || approved.value[id] === version) return
    const valid = () => current === generation && token === selectionJob && selected.value === id && sessions.get(card.source) === session && versions.value[id] === version && !!documents.value[id] && !loading.value[id]
    try {
      await App.AcknowledgeLyricsPreview(session, id, version)
      if (valid()) approved.value[id] = version
    } catch (e) { if (valid()) error.value = String(e) }
  }
  async function adopt() {
    const card = selection.value, current = generation
    if (!card || !canAdopt.value) return
    const session = sessions.get(card.source)
    if (!session) return
    busy.value = true; error.value = ''; notice.value = ''
    try {
      const rev = await App.AdoptLyrics(session, card.id, revision)
      if (current !== generation || sessions.get(card.source) !== session) return
      revision = rev; notice.value = '歌词已应用到当前歌曲'
    } catch (e) { if (current === generation) error.value = `${String(e)}（可关闭后重新打开歌词搜索再试）` }
    finally { if (current === generation) busy.value = false }
  }
  return { target, title, artist, album, sources, sourceOptions, results, cards, documents, failures, loading, selected, selection, document, documentVersion, canAdopt, error, notice, ready, busy, open, close, search, select, fetchSelected, acknowledge, adopt }
})
