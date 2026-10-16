import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App, type Models } from '../../src/api'
import { useLyricsSearchStore } from '../../src/stores/lyricsSearch'
vi.mock('../../src/api', () => ({ App: {
  LyricsRevision: vi.fn(), CreateLyricsSearch: vi.fn(), CancelLyricsSearch: vi.fn(), SearchLyrics: vi.fn(), FetchLyrics: vi.fn(), AdoptLyrics: vi.fn(),
} }))
const detail = (id: number) => ({ track: { id, title: '歌曲', artist: '歌手' } }) as Models.app.TrackDetail
const card = (source: string) => ({ id: source, source, title: source, artist: '', album: '', durationMs: 1000 }) as Models.lyrics.Candidate
const deferred = <T>() => { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r }); return { promise, resolve } }
beforeEach(() => {
  setActivePinia(createPinia()); vi.resetAllMocks()
  let id = 0
  vi.mocked(App.LyricsRevision).mockResolvedValue(4)
  vi.mocked(App.CreateLyricsSearch).mockImplementation(async () => `session-${++id}`)
  vi.mocked(App.CancelLyricsSearch).mockResolvedValue()
  vi.mocked(App.SearchLyrics).mockImplementation(async (_, source) => [card(source)])
  vi.mocked(App.FetchLyrics).mockResolvedValue({ text: '[00:01]歌词', format: 'lrc' })
  vi.mocked(App.AdoptLyrics).mockResolvedValue(5)
})
it('keeps source sessions independent and applies only the selected fetched candidate at its original revision', async () => {
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
  await store.select('qq'); await store.search('163'); await store.select('qq'); await store.adopt()
  expect(App.AdoptLyrics).toHaveBeenCalledWith('session-1', 'qq', 4)
  expect(store.notice).toContain('已应用')
  await store.search('163')
  expect(App.CancelLyricsSearch).not.toHaveBeenCalledWith('session-1')
  expect(store.documents.qq?.text).toContain('歌词')
})
it('cancels a session created after close and does not launch its search', async () => {
  const pending = deferred<string>(); vi.mocked(App.CreateLyricsSearch).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); const work = store.search('qq')
  await vi.waitFor(() => expect(App.CreateLyricsSearch).toHaveBeenCalled())
  await store.close(); pending.resolve('late'); await work
  expect(App.CancelLyricsSearch).toHaveBeenCalledWith('late'); expect(App.SearchLyrics).not.toHaveBeenCalled(); expect(store.target).toBeNull()
})
it('ignores late documents when a new search replaces the source', async () => {
  const pending = deferred<Models.lyrics.Document>(); vi.mocked(App.FetchLyrics).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq')
  const fetching = store.select('qq')
  vi.mocked(App.SearchLyrics).mockResolvedValue([]); await store.search('qq')
  pending.resolve({ text: 'old', format: 'text' }); await fetching; await Promise.resolve()
  expect(store.documents).toEqual({}); expect(store.cards).toEqual([])
})
it('does not allow adoption of missing lyrics and preserves a recoverable conflict error', async () => {
  vi.mocked(App.FetchLyrics).mockRejectedValue(new Error('暂无歌词'))
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq'); await store.adopt()
  expect(App.AdoptLyrics).not.toHaveBeenCalled(); expect(store.failures.qq).toContain('暂无歌词')
  vi.mocked(App.FetchLyrics).mockResolvedValue({ text: 'recovered', format: 'text' }); await store.select('qq')
  vi.mocked(App.AdoptLyrics).mockRejectedValue(new Error('修订冲突，请重新打开'))
  await store.adopt(); expect(store.error).toContain('修订冲突'); expect(store.document?.text).toBe('recovered')
})
it('late adoption never updates another target or revision', async () => {
  const pending = deferred<number>(); vi.mocked(App.AdoptLyrics).mockReturnValue(pending.promise)
  const store = useLyricsSearchStore(); await store.open(detail(1)); await store.search('qq'); await store.select('qq')
  const adopting = store.adopt(); await store.open(detail(2)); pending.resolve(100); await adopting
  expect(store.target?.track.id).toBe(2); expect(store.notice).toBe(''); expect(store.busy).toBe(false)
})
