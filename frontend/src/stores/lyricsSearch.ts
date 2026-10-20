import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { App, type Models } from '@/api'
import { useSourceSelection } from '@/lib/sourceSelection'

type Candidate = Omit<Models.lyrics.Candidate, 'convertValues'>
type Document = Omit<Models.lyrics.Document, 'convertValues'>
type Result = { state: 'loading' | 'ok' | 'error'; cards: Candidate[]; error: string }
export const lyricFormat = (format?: string) => ({ text: '文本', lrc: 'LRC 时间轴歌词', enhanced_lrc: '增强型 LRC' }[format ?? ''] ?? '待获取歌词')

export const useLyricsSearchStore = defineStore('lyricsSearch', () => {
  const target = ref<Models.app.TrackDetail | null>(null)
  const title = ref(''), artist = ref(''), album = ref('')
  const sourceSelection = useSourceSelection<Models.lyrics.Source>()
  const { selected: sources, options: sourceOptions } = sourceSelection
  const results = ref<Record<string, Result>>({})
  const documents = ref<Record<string, Document>>({})
  const failures = ref<Record<string, string>>({})
  const loading = ref<Record<string, boolean>>({})
  const selected = ref<string | null>(null), error = ref(''), notice = ref(''), ready = ref(false), busy = ref(false)
  const cards = computed(() => Object.values(results.value).flatMap(r => r.cards).sort((a, b) =>
    Number(b.highConfidence) - Number(a.highConfidence) || b.score - a.score || a.source.localeCompare(b.source)))
  const selection = computed(() => cards.value.find(c => c.id === selected.value))
  const document = computed(() => selected.value ? documents.value[selected.value] : undefined)
  const sessions = new Map<string, string>()
  const jobs = new Map<string, number>()
  let generation = 0, revision = 0, job = 0
  async function cancel(ids: string[]) {
    // A disconnected bridge must not strand the next search in loading state.
    // Generation/session checks still discard late replies if cancellation fails.
    await Promise.allSettled(ids.map(id => App.CancelLyricsSearch(id)))
  }
  async function close() {
    generation++
    const ids = [...sessions.values()]; sessions.clear(); jobs.clear()
    target.value = null; ready.value = false; sourceOptions.value = []; results.value = {}; documents.value = {}; failures.value = {}; loading.value = {}
    selected.value = null; error.value = ''; notice.value = ''; busy.value = false
    await cancel(ids)
  }
  async function open(detail: Models.app.TrackDetail) {
    const closing = close(), current = generation
    target.value = detail; title.value = detail.track.title; artist.value = detail.track.artist ?? ''; album.value = detail.track.album ?? ''
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
    if (![title.value, artist.value, album.value].some(s => s.trim())) { error.value = '请输入歌词搜索条件'; return }
    const current = generation, trackID = target.value.track.id
    const query = { title: title.value, artist: artist.value, album: album.value, durationMs: target.value.track.durationMs ?? target.value.metadata?.durationMs ?? 0 }
    error.value = ''; notice.value = ''
    const previous = only ? [sessions.get(only)].filter((v): v is string => !!v) : [...sessions.values()]
    if (!only) { sessions.clear(); jobs.clear(); results.value = {}; documents.value = {}; failures.value = {}; loading.value = {}; selected.value = null }
    else {
      sessions.delete(only)
      for (const card of results.value[only]?.cards ?? []) {
        delete documents.value[card.id]; delete failures.value[card.id]; delete loading.value[card.id]
        if (selected.value === card.id) selected.value = null
      }
    }
    // Register before waiting: a repeated search/close invalidates late Create calls.
    const work = chosen.map(source => { const token = ++job; jobs.set(source, token); results.value[source] = { state: 'loading', cards: [], error: '' }; return { source, token } })
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
        results.value[source] = { state: 'ok', cards: found, error: '' }
      } catch (e) {
        if (!valid()) return
        results.value[source] = { state: 'error', cards: [], error: String(e) }
        if (id) { sessions.delete(source); await cancel([id]) }
      }
    }))
    // Wait for all current sources: network speed is not match confidence.
    // A manual selection, a replacement search or a closed dialog wins.
    if (current !== generation || work.some(({ source, token }) => jobs.get(source) !== token) ||
      selected.value || Object.values(results.value).some(r => r.state === 'loading')) return
    const best = cards.value.find(c => c.highConfidence)
    if (best) await select(best.id)
  }
  async function select(id: string) {
    const card = cards.value.find(c => c.id === id)
    if (!card || busy.value) return
    selected.value = id
    const session = sessions.get(card.source), current = generation
    if (!session || documents.value[id] || loading.value[id]) return
    loading.value[id] = true; delete failures.value[id]
    const valid = () => current === generation && sessions.get(card.source) === session
    try {
      const doc = await App.FetchLyrics(session, id)
      if (valid()) documents.value[id] = doc
    } catch (e) { if (valid()) failures.value[id] = String(e) }
    finally { if (valid()) loading.value[id] = false }
  }
  async function adopt() {
    const card = selection.value, current = generation
    if (!card || !document.value || busy.value) return
    const session = sessions.get(card.source)
    if (!session) return
    busy.value = true; error.value = ''; notice.value = ''
    try {
      const rev = await App.AdoptLyrics(session, card.id, revision)
      if (current !== generation) return
      revision = rev; notice.value = '歌词已应用到当前歌曲'
    } catch (e) { if (current === generation) error.value = `${String(e)}（可关闭后重新打开歌词搜索再试）` }
    finally { if (current === generation) busy.value = false }
  }
  return { target, title, artist, album, sources, sourceOptions, results, cards, documents, failures, loading, selected, selection, document, error, notice, ready, busy, open, close, search, select, adopt }
})
