import { describe, expect, it } from 'vitest'
import fixture from '../../../testdata/lyrics/ranking.json'
import { compareLyrics } from '../../src/lib/lyricsRanking'

describe('lyrics cross-source ranking', () => {
  it('uses the Go fixture without locale, source groups or unchecked zero scores', () => {
    for (let shift = 0; shift < fixture.rows.length; shift++) {
      const rows = [...fixture.rows.slice(shift), ...fixture.rows.slice(0, shift)].reverse()
      expect(rows.sort(compareLyrics).map(row => row.id)).toEqual(fixture.expected)
    }
    expect(fixture.rows.find(row => row.id === 'unscored')?.totalScore).toBeNull()
  })
})
