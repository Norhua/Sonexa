import { expect, it } from 'vitest'
import { lyricChannelStatus, lyricChannelBasis, lyricTime } from '../../src/lib/lyricsPresentation'
it('reports all channel states without a source-specific branch', () => {
  for (const [status, expected] of Object.entries({ available: '有', unavailable: '未提供', pending: '待检查', loading: '获取中', error: '失败', unknown: '无法确认' })) {
    expect(lyricChannelStatus({ status, origin: 'unknown', syncStatus: 'unknown' })).toBe(expected)
  }
  expect(lyricChannelStatus({ status: 'available', origin: 'embedded', syncStatus: 'partial', diagnostics: [{ code: 'inferred', ruleVersion: 'custom-v1', layer: 2, count: 4 }] })).toBe('有（启发式识别、部分）')
  expect(lyricChannelBasis({ status: 'available', origin: 'embedded', syncStatus: 'synced', diagnostics: [{ code: 'inferred', ruleVersion: 'custom-v1', layer: 2, count: 4 }] })).toContain('第 2 层 · 4 行')
  expect(lyricTime(12123)).toBe('00:12.123'); expect(lyricTime()).toBe('—')
})
