import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, EventsOn, type Models } from '../../src/api'
import { useLyricsSearchStore } from '../../src/stores/lyricsSearch'
import { lyricSourceStatus, type LyricProgress } from '../../src/lib/lyricsPresentation'
vi.mock('../../src/api', () => ({ App: {
  ListLyricsSources: vi.fn(), LyricsRevision: vi.fn(), CreateLyricsSearch: vi.fn(), CancelLyricsSearch: vi.fn(), SearchLyrics: vi.fn(), FetchLyrics: vi.fn(), AdoptLyrics: vi.fn(),
  StartLyricsPrefetch: vi.fn(), LyricsSearchSnapshot: vi.fn(), AcknowledgeLyricsPreview: vi.fn(), RefreshLyrics: vi.fn(),
}, Events: { LyricsProgress: 'lyrics:progress' }, EventsOn: vi.fn() }))
const detail = (id: number) => ({ track: { id, title: '歌曲', artist: '歌手' } }) as Models.app.TrackDetail
const card = (source: string, highConfidence = false, matchScore = 55, totalScore = matchScore + 10) => ({ id: source, source, title: source, artist: '', album: '', durationMs: 1000, highConfidence, matchScore, totalScore, autoPreviewEligible: highConfidence, sortKey: source, scoreTerms: [], matchTerms: [], matchNotes: [] }) as Models.lyrics.Candidate
const preview = (text = '歌词', version = 1) => ({ document: { text, format: 'text', offsetMs: 0, rows: [{ text, channel: 'original' }] }, version }) as Models.app.LyricsPreview
const deferred = <T>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
let snapshots: Map<string, LyricProgress>, listeners: Set<(event: LyricProgress) => void>
const progress = (id: string, source: string, rows: Models.lyrics.Candidate[], sequence = 4, phase = 'done'): LyricProgress => ({ sessionId: id, source, sequence, phase, searched: rows.length, selected: rows.length, success: rows.length, noLyrics: 0, failed: 0, incomplete: 0, candidates: rows.map(candidate => ({ candidate, fetchState: 'ready', prefetch: true, format: 'lrc', documentVersion: 1, wordTiming: 'unavailable', translation: { status: 'available', origin: 'independent', syncStatus: 'unsynced' }, romanization: { status: 'unavailable', origin: 'independent', syncStatus: 'unknown' } })) })
function emit(event: LyricProgress) { for (const fn of listeners) fn(event) }
async function acknowledge(store: ReturnType<typeof useLyricsSearchStore>) { await store.acknowledge(store.selected!, store.documentVersion) }
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks(); snapshots = new Map(); listeners = new Set()
  let id = 0
  vi.mocked(EventsOn).mockImplementation((_, fn) => { listeners.add(fn); return () => { listeners.delete(fn) } })
  vi.mocked(App.ListLyricsSources).mockResolvedValue(['163', 'qq', 'kugou'].map(id => ({ id, name: id })))
  vi.mocked(App.LyricsRevision).mockResolvedValue(4)
  vi.mocked(App.CreateLyricsSearch).mockImplementation(async () => `session-${++id}`)
  vi.mocked(App.CancelLyricsSearch).mockResolvedValue()
  vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => { const rows = [card(source)]; snapshots.set(id, progress(id, source, rows)); return rows })
  vi.mocked(App.StartLyricsPrefetch).mockImplementation(async id => snapshots.get(id)! as Models.app.LyricsProgress)
  vi.mocked(App.LyricsSearchSnapshot).mockImplementation(async id => snapshots.get(id)! as Models.app.LyricsProgress)
  vi.mocked(App.FetchLyrics).mockResolvedValue(preview())
  vi.mocked(App.AcknowledgeLyricsPreview).mockResolvedValue()
  vi.mocked(App.AdoptLyrics).mockResolvedValue(5)
})
it('keeps source sessions independent and requires display acknowledgement before adoption', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq')
  await store.adopt(); expect(App.AdoptLyrics).not.toHaveBeenCalled(); expect(store.canAdopt).toBe(false)
  await acknowledge(store); await store.search('163'); await store.select('qq'); await store.adopt()
  expect(App.AdoptLyrics).toHaveBeenCalledWith('session-1', 'qq', 4)
  expect(store.notice).toContain('已应用')
  await store.search('163'); expect(App.CancelLyricsSearch).not.toHaveBeenCalledWith('session-1')
  expect(store.documents.qq?.text).toContain('歌词'); expect(App.FetchLyrics).toHaveBeenCalledTimes(1)
})
it('cancels a session created after close and does not launch its search', async () => {
  const pending = deferred<string>(); vi.mocked(App.CreateLyricsSearch).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); const work = store.search('qq')
  await vi.waitFor(() => expect(App.CreateLyricsSearch).toHaveBeenCalled())
  await store.close(); pending.resolve('late'); await work
  expect(App.CancelLyricsSearch).toHaveBeenCalledWith('late'); expect(App.SearchLyrics).not.toHaveBeenCalled(); expect(store.target).toBeNull()
})
it('ignores late documents when a new search replaces the source', async () => {
  const pending = deferred<Models.app.LyricsPreview>(); vi.mocked(App.FetchLyrics).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); const fetching = store.select('qq')
  vi.mocked(App.SearchLyrics).mockImplementation(async id => { snapshots.set(id, progress(id, 'qq', [])); return [] }); await store.search('qq')
  pending.resolve(preview('old')); await fetching
  expect(store.documents).toEqual({}); expect(store.cards).toEqual([])
})
it('does not allow adoption of missing lyrics and uses recoverable feedback instead of raw errors', async () => {
  vi.mocked(App.FetchLyrics).mockRejectedValue(new Error('暂无歌词：upstream.internal diagnostic'))
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq'); await store.adopt()
  expect(App.AdoptLyrics).not.toHaveBeenCalled(); expect(store.failures.qq).toBe('歌词暂不可用，请重试或选择其他候选。')
  vi.mocked(App.FetchLyrics).mockResolvedValue(preview('recovered')); await store.select('qq'); await acknowledge(store)
  vi.mocked(App.AdoptLyrics).mockRejectedValue(new Error('修订冲突，请重新打开'))
  await store.adopt(); expect(store.error).toBe('歌词未能应用，请关闭后重新打开歌词搜索再试。'); expect(store.document?.text).toBe('recovered')
})
it('does not expose backend initialization or acknowledgement diagnostics', async () => {
  vi.mocked(App.LyricsRevision).mockRejectedValueOnce(new Error('database /private/lyrics.db unavailable'))
  const store = useLyricsSearchStore(); await store.open(detail(1))
  expect(store.error).toBe('无法打开歌词搜索，请关闭后重试。'); expect(store.ready).toBe(false)
  await store.open(detail(1)); await store.search('qq'); await store.select('qq')
  vi.mocked(App.AcknowledgeLyricsPreview).mockRejectedValueOnce(new Error('internal session/version mismatch'))
  await acknowledge(store)
  expect(store.error).toBe('预览确认失败，请重新选择候选后再试。'); expect(store.canAdopt).toBe(false)
  await acknowledge(store); expect(store.canAdopt).toBe(true)
})
it('late adoption never updates another target or revision', async () => {
  const pending = deferred<number>(); vi.mocked(App.AdoptLyrics).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq'); await acknowledge(store)
  const adopting = store.adopt(); await store.open(detail(2)); pending.resolve(100); await adopting
  expect(store.target?.track.id).toBe(2); expect(store.notice).toBe(''); expect(store.busy).toBe(false)
})
it('passes target duration, prefetches without selection, and freezes submitted retry query', async () => {
  const target = detail(1); target.track.durationMs = 321000
  const store = useLyricsSearchStore(); await store.open(target); await store.search('qq')
  expect(App.SearchLyrics).toHaveBeenCalledWith('session-1', 'qq', expect.objectContaining({ durationMs: 321000 }))
  expect(App.StartLyricsPrefetch).toHaveBeenCalledWith('session-1'); expect(App.LyricsSearchSnapshot).toHaveBeenCalledTimes(1)
  expect(store.selected).toBeNull(); expect(store.cards[0]?.translation.status).toBe('available'); expect(App.FetchLyrics).not.toHaveBeenCalled()
  expect(store.durationInput).toBe('5:21')
  store.title = ''; store.artist = ''; store.album = ''; store.durationInput = 'invalid'; await store.search('qq')
  expect(App.SearchLyrics).toHaveBeenLastCalledWith('session-2', 'qq', expect.objectContaining({ title: '歌曲', artist: '歌手', durationMs: 321000 }))
})
it('submits edited duration only on a new search and restores defaults on reopen', async () => {
  const target = detail(1); target.track.durationMs = 0; target.metadata = { durationMs: 299123 } as Models.app.TrackDetail['metadata']
  const store = useLyricsSearchStore(); await store.open(target)
  expect(store.durationInput).toBe('4:59.123')
  store.durationInput = '5:01.5'; await store.search()
  expect(App.SearchLyrics).toHaveBeenLastCalledWith(expect.any(String), expect.any(String), expect.objectContaining({ durationMs: 301500 }))
  store.durationInput = 'invalid'; await store.search()
  expect(App.SearchLyrics).toHaveBeenCalledTimes(3); expect(store.error).toContain('时长请输入')
  store.durationInput = ''; await store.search()
  expect(App.SearchLyrics).toHaveBeenLastCalledWith(expect.any(String), expect.any(String), expect.objectContaining({ durationMs: 0 }))
  expect(target.metadata!.durationMs).toBe(299123)
  await store.open(target); expect(store.durationInput).toBe('4:59.123')
  await store.open(detail(2)); expect(store.durationInput).toBe('')
})
it('waits for search AND prefetch terminals before automatic preview, independent of speed', async () => {
  const slow = deferred<Models.lyrics.Candidate[]>()
  vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => {
    const rows = source === 'qq' ? await slow.promise : [card(source, true, 75)]
    const event = progress(id, source, rows, 3, source === 'qq' ? 'prefetching' : 'done')
    if (source === 'qq') event.candidates[0]!.fetchState = 'loading'
    snapshots.set(id, event); return rows
  })
  const store = useLyricsSearchStore(); await store.open(detail(1)); store.sources = ['163', 'qq']; const searching = store.search()
  await vi.waitFor(() => expect(store.results['163']?.phase).toBe('done')); expect(App.FetchLyrics).not.toHaveBeenCalled()
  slow.resolve([card('qq', true, 100)]); await searching; expect(App.FetchLyrics).not.toHaveBeenCalled()
  emit(progress('session-2', 'qq', [card('qq', true, 100)], 5))
  await vi.waitFor(() => expect(store.selected).toBe('qq')); expect(App.FetchLyrics).toHaveBeenCalledTimes(1)
})
it('accepts events before search/start responses and rejects older snapshots', async () => {
  vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => {
    const rows = [card(source, false, 80, 125)]
    snapshots.set(id, progress(id, source, [card(source, false, 80, 90)], 2, 'prefetching'))
    emit(progress(id, source, rows, 8)); return rows
  })
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
  expect(store.cards[0]?.totalScore).toBe(125); expect(store.results.qq?.sequence).toBe(8); expect(store.results.qq?.phase).toBe('done')
})
it('never overrides manual selection when later high-score results arrive', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search(); await store.select('163')
  emit(progress('session-2', 'qq', [card('qq', true, 100, 150)], 10))
  expect(store.selected).toBe('163'); expect(store.cards[0]?.id).toBe('qq'); expect(App.FetchLyrics).toHaveBeenCalledTimes(1)
})
it('source failure does not prevent a confident successful source from previewing', async () => {
  vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => {
    if (source === 'qq') throw new Error('unavailable')
    const rows = [card(source, true, 80)]; snapshots.set(id, progress(id, source, rows)); return rows
  })
  const store = useLyricsSearchStore(); await store.open(detail(1)); store.sources = ['qq', '163']; await store.search()
  expect(store.selected).toBe('163'); expect(store.results.qq?.state).toBe('error')
})
it('retains successful search counts through partial/stopped rounds and source retry', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search(); await store.select('163'); await acknowledge(store)
  for (const [i, phase] of ['partial', 'stopped'].entries()) {
    const event = progress('session-2', 'qq', [card('qq')], 10 + i, phase)
    event.failed = 1; event.success = 0
    Object.assign(event.candidates[0]!, { fetchState: 'error', errorKind: 'upstream', translation: { status: 'error' }, romanization: { status: 'error' } })
    emit(event)
    expect(lyricSourceStatus(store.results.qq!)).toEqual({ count: 1, failed: false, label: '共 1 个候选' })
    expect(store.cards.find(c => c.id === 'qq')?.errorKind).toBe('upstream')
  }
  vi.mocked(App.SearchLyrics).mockRejectedValueOnce(new Error('search failed'))
  await store.search('qq')
  expect(lyricSourceStatus(store.results.qq!).failed).toBe(true)
  expect(store.selected).toBe('163'); expect(store.canAdopt).toBe(true)
  await store.search('qq')
  expect(lyricSourceStatus(store.results.qq!)).toEqual({ count: 1, failed: false, label: '共 1 个候选' })
  expect(store.selected).toBe('163'); expect(store.canAdopt).toBe(true)
})
for (const operation of ['StartLyricsPrefetch', 'LyricsSearchSnapshot'] as const) {
  for (const empty of [false, true]) {
    it(`does not turn a successful ${empty ? 'empty' : 'nonempty'} search into failure when ${operation} rejects`, async () => {
      if (empty) vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => { snapshots.set(id, progress(id, source, [])); return [] })
      vi.mocked(App[operation]).mockRejectedValueOnce(new Error('later stage failed'))
      const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
      expect(store.results.qq?.state).toBe('ok'); expect(store.results.qq?.phase).toBe('error')
      expect(store.results.qq?.settled).toBe(true)
      expect(lyricSourceStatus(store.results.qq!)).toEqual({ count: empty ? 0 : 1, failed: false, label: `共 ${empty ? 0 : 1} 个候选` })
      expect(App.CancelLyricsSearch).toHaveBeenCalledWith('session-1')
      expect(store.canAdopt).toBe(false)
      await store.search('qq')
      expect(store.results.qq?.phase).toBe('done')
    })
  }
}
it('does not mistake a searching progress event for a successful empty search', async () => {
  const pending = deferred<Models.lyrics.Candidate[]>()
  vi.mocked(App.SearchLyrics).mockImplementation(async (id, source) => {
    emit(progress(id, source, [], 1, 'searching'))
    const rows = await pending.promise
    snapshots.set(id, progress(id, source, rows, 2, 'done')); return rows
  })
  const store = useLyricsSearchStore(); await store.open(detail(1)); const work = store.search('qq')
  await vi.waitFor(() => expect(store.results.qq?.sequence).toBe(1))
  expect(lyricSourceStatus(store.results.qq!)).toEqual({ count: undefined, failed: false, label: '正在搜索' })
  pending.resolve([]); await work
  expect(lyricSourceStatus(store.results.qq!)).toEqual({ count: 0, failed: false, label: '共 0 个候选' })
})
it('late superseded search and events cannot trigger automatic selection', async () => {
  const old = deferred<Models.lyrics.Candidate[]>()
  vi.mocked(App.SearchLyrics).mockReturnValueOnce(old.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); const searching = store.search('qq')
  await vi.waitFor(() => expect(App.SearchLyrics).toHaveBeenCalledTimes(1)); await store.search('qq')
  emit(progress('session-1', 'qq', [card('qq', true, 100)], 999)); old.resolve([card('qq', true, 100)]); await searching
  expect(store.selected).toBeNull(); expect(App.FetchLyrics).not.toHaveBeenCalled(); expect(store.results.qq?.sequence).toBe(4)
})
it('releases only its listener on reopen and ignores captured old callbacks', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); const callback = vi.mocked(EventsOn).mock.calls[0]![1]
  await store.open(detail(2)); await store.search('qq'); expect(listeners.size).toBe(1)
  callback(progress('session-1', 'qq', [card('qq', true)], 999)); expect(store.results.qq?.sequence).toBe(4)
  await store.close(); expect(listeners.size).toBe(0)
})
it('keeps manual unprefetched items unscored until explicit fetch and preserves selection', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
  const event = progress('session-1', 'qq', [card('qq')], 5)
  Object.assign(event.candidates[0]!, { fetchState: 'pending', prefetch: false, documentVersion: 0 })
  event.candidates[0]!.candidate.totalScore = null; emit(event)
  await store.select('qq'); expect(App.FetchLyrics).not.toHaveBeenCalled(); expect(store.cards[0]?.totalScore).toBeNull()
  await store.fetchSelected(); expect(App.FetchLyrics).toHaveBeenCalledTimes(1); expect(store.selected).toBe('qq')
})
it('rejects stale document/approval events after a refreshed version', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq'); await acknowledge(store)
  const event = progress('session-1', 'qq', [card('qq', false, 55, 100)], 7); event.candidates[0]!.documentVersion = 2
  emit(event); expect(store.document).toBeUndefined(); expect(store.canAdopt).toBe(false)
  vi.mocked(App.FetchLyrics).mockResolvedValue(preview('new', 2)); await store.select('qq'); await store.acknowledge('qq', 1)
  expect(store.canAdopt).toBe(false)
  await acknowledge(store); emit(progress('session-1', 'qq', [card('qq')], 6))
  expect(store.document?.text).toBe('new'); expect(store.documentVersion).toBe(2); expect(store.canAdopt).toBe(true)
})
it('late acknowledgement after manual selection changes does not grant UI adoption', async () => {
  const pending = deferred<void>(); vi.mocked(App.AcknowledgeLyricsPreview).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search(); await store.select('qq')
  const approving = acknowledge(store); await store.select('163'); pending.resolve(); await approving
  await store.select('qq'); expect(store.canAdopt).toBe(false)
})
it('inflight queued selection waits for the existing backend fetch without another start', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
  const event = progress('session-1', 'qq', [card('qq')], 6, 'prefetching'); event.candidates[0]!.fetchState = 'queued'; emit(event)
  const waiting = deferred<Models.app.LyricsPreview>(); vi.mocked(App.FetchLyrics).mockReturnValue(waiting.promise)
  const selecting = store.select('qq'); await store.select('qq'); expect(App.FetchLyrics).toHaveBeenCalledTimes(1)
  waiting.resolve(preview()); await selecting; expect(App.StartLyricsPrefetch).toHaveBeenCalledTimes(1)
})
