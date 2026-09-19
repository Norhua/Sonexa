import { describe, expect, it } from 'vitest'
import { creditParts } from '../../src/lib/creditParts'

describe('UTF-8 credit spans', () => {
  it('preserves Japanese names, CV notation, emoji and join phrases', () => {
    const a = '涼宮ハルヒ(CV:平野綾)', b = '初音ミク🎵'
    const text = `${a} feat. ${b}`
    const bytes = (s: string) => new TextEncoder().encode(s).length
    expect(creditParts(text, [
      { start: 0, end: bytes(a), artistId: 1, text: a },
      { start: bytes(a + ' feat. '), end: bytes(text), artistId: 2, text: b },
    ])).toEqual([{ text: a, artistId: 1 }, { text: ' feat. ' }, { text: b, artistId: 2 }])
  })
  it('keeps invalid, overlapping, unmatched or stale spans as plain text', () => {
    expect(creditParts('ハトリアヤ', [
      { start: 1, end: 3, artistId: 1 },
      { start: 0, end: 999, artistId: 2 },
      { start: 0, end: 15, artistId: 3, text: 'stale' },
    ])).toEqual([{ text: 'ハトリアヤ' }])
    expect(creditParts('ABC', [{ start: 0, end: 2, artistId: 1 }, { start: 1, end: 3, artistId: 2 }]))
      .toEqual([{ text: 'AB', artistId: 1 }, { text: 'C' }])
    expect(creditParts('unknown', [])).toEqual([{ text: 'unknown' }])
    expect(creditParts('', [])).toEqual([])
  })
})
