import { expect, it } from 'vitest'
import { lyricChannelStatus, lyricChannelBasis, lyricReadingRows, lyricTime, lyricDurationInput, parseLyricDuration, lyricSourceStatus, lyricScoreDescription, pendingLyricCard, type LyricCandidate, type LyricChannel, type LyricDocument } from '../../src/lib/lyricsPresentation'
it('parses search durations without rounding away milliseconds or accepting malformed times', () => {
  for (const [input, expected] of [['', 0], ['  ', 0], ['4:59', 299000], ['299', 299000], ['4:59.123', 299123], ['299.1', 299100], ['1440:00', 86400000]] as const) {
    expect(parseLyricDuration(input)).toBe(expected)
    expect(parseLyricDuration(lyricDurationInput(expected))).toBe(expected)
  }
  for (const input of ['0', '-1', 'NaN', 'Infinity', '1:60', '4:9', '1:01:00', '1e3', '2.1234', '86401', '9'.repeat(100)]) expect(parseLyricDuration(input)).toBeUndefined()
  expect(lyricDurationInput()).toBe(''); expect(lyricDurationInput(null)).toBe('')
})
it('shows total candidates rather than prefetch successes and distinguishes errors from no lyrics', () => {
  const candidate = { id: 'c', matchScore: 80, totalScore: 110, matchNotes: ['曲名相符', '艺术家不同或缺失，需确认'], highConfidence: false } as LyricCandidate
  const card = pendingLyricCard(candidate, true)
  const result = { state: 'ok', phase: 'done', cards: Array.from({ length: 7 }, () => card) }
  expect(lyricSourceStatus(result)).toEqual({ count: 7, failed: false, label: '共 7 个候选' })
  expect(lyricSourceStatus({ ...result, cards: [] })).toEqual({ count: 0, failed: false, label: '共 0 个候选' })
  expect(lyricSourceStatus({ ...result, state: 'loading', phase: 'searching', cards: [] })).toEqual({ count: undefined, failed: false, label: '正在搜索' })
  expect(lyricSourceStatus({ ...result, state: 'error', phase: 'error', cards: [] })).toEqual({ count: undefined, failed: true, label: '获取异常，可重试' })
  expect(lyricSourceStatus({ ...result, phase: 'stopped' }).failed).toBe(true)
  expect(lyricSourceStatus({ ...result, phase: 'partial', cards: [{ ...card, fetchState: 'no_lyrics' }] }).failed).toBe(false)
  expect(lyricSourceStatus({ ...result, phase: 'partial', cards: [{ ...card, fetchState: 'error' }] }).failed).toBe(true)
  expect(lyricSourceStatus({ ...result, cards: [{ ...card, translation: { ...card.translation, status: 'error' } }] }).failed).toBe(true)
  expect(lyricSourceStatus({ ...result, cards: [{ ...card, wordTiming: 'error' }] }).failed).toBe(true)
  expect(lyricSourceStatus({ ...result, cards: [{ ...card, fetchState: 'ready', errorKind: 'format' }] }).failed).toBe(true)
  expect(lyricSourceStatus({ ...result, state: 'error' }).label).toBe('获取异常，共 7 个候选，可重试')
  expect(lyricScoreDescription(candidate)).toBe('歌曲匹配 80 分・歌词综合 110，曲名相符；艺术家不同或缺失，需确认（分数不是概率）')
  expect(lyricScoreDescription({ ...candidate, totalScore: null })).toContain('歌词综合 —')
  expect(lyricScoreDescription({ ...candidate, totalScore: -10 })).toContain('歌词综合 -10')
})

it('reports all channel states without a source-specific branch', () => {
  for (const [status, expected] of Object.entries({ available: '有', unavailable: '未提供', pending: '待检查', loading: '获取中', error: '失败', unknown: '无法确认' })) {
    expect(lyricChannelStatus({ status, origin: 'unknown', syncStatus: 'unknown' })).toBe(expected)
  }
  expect(lyricChannelStatus({ status: 'available', origin: 'embedded', syncStatus: 'partial', diagnostics: [{ code: 'inferred', ruleVersion: 'custom-v1', layer: 2, count: 4 }] })).toBe('有（启发式识别、部分）')
  expect(lyricChannelBasis({ status: 'available', origin: 'embedded', syncStatus: 'synced', diagnostics: [{ code: 'inferred', ruleVersion: 'custom-v1', layer: 2, count: 4 }] })).toContain('第 2 层 · 4 行')
  expect(lyricTime(12123)).toBe('00:12.123'); expect(lyricTime()).toBe('—')
})
it('distinguishes timestamp precision alignment from heuristic channel identification', () => {
  const channel: LyricChannel = { status: 'available', origin: 'independent', syncStatus: 'synced', diagnostics: [{ code: 'time_precision_alignment', ruleVersion: 'time-precision-v1', layer: 1, count: 3 }] }
  expect(lyricChannelStatus(channel)).toBe('有（时间精度对齐）')
  expect(lyricChannelBasis(channel)).toContain('±10 毫秒内唯一且无交叉')
  expect(lyricChannelBasis(channel)).not.toContain('经验推断')
  channel.diagnostics!.push({ code: 'heuristic_translation', ruleVersion: 'embedded-v1', layer: 2, count: 4 })
  expect(lyricChannelStatus(channel)).toBe('有（启发式识别、时间精度对齐）')
  expect(lyricChannelBasis(channel)).toContain('经验推断')
})
it('projects three columns using only existing associations and keeps independent extras separate', () => {
  const rows: LyricDocument['rows'] = [
    { timeMs: 0, text: '  Hello (world) <b>✨</b>  ', channel: 'original', translation: ['  你好  ', '第二行'], romanization: ['hello world'] },
    { timeMs: 500, text: '独立译文', channel: 'translation' },
    { timeMs: 500, text: '独立音译', channel: 'romanization' },
    { text: '无时间译文', channel: 'translation' },
    { text: '无时间音译', channel: 'romanization' },
    { text: '谐音未知', channel: 'additional' },
    { text: '', channel: 'original' },
  ]
  const before = structuredClone(rows)
  expect(lyricReadingRows(rows, 'translation')).toEqual([
    { timeMs: 0, text: rows[0]!.text, extra: ['  你好  ', '第二行'], note: '' },
    { timeMs: 500, text: '', extra: ['独立译文'], note: '翻译 · 独立行' },
    { text: '', extra: ['无时间译文'], note: '翻译 · 未同步' },
    { text: '', extra: ['谐音未知'], note: '附加文本（归属未确认） · 未同步' },
    { text: '', extra: [], note: '' },
  ])
  expect(lyricReadingRows(rows, 'romanization')).toEqual([
    { timeMs: 0, text: rows[0]!.text, extra: ['hello world'], note: '' },
    { timeMs: 500, text: '', extra: ['独立音译'], note: '罗马音 · 独立行' },
    { text: '', extra: ['无时间音译'], note: '罗马音 · 未同步' },
    { text: '', extra: ['谐音未知'], note: '附加文本（归属未确认） · 未同步' },
    { text: '', extra: [], note: '' },
  ])
  expect(rows).toEqual(before)
})
it('keeps plain lyrics and empty extra cells when the chosen channel is absent', () => {
  const rows = [{ text: '第一段\n\n第二段', channel: 'original' }]
  for (const channel of ['translation', 'romanization'] as const) {
    expect(lyricReadingRows(rows, channel)).toEqual([{ text: '第一段\n\n第二段', extra: [], note: '' }])
  }
})
