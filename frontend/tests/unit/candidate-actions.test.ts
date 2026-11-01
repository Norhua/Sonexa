import { describe, expect, it } from 'vitest'
import type { Models } from '../../src/api'
import { candidateActionEnabled, candidateActionLabel, candidateLocalTarget } from '../../src/lib/candidateActions'

function row(action: string, state: string, id = 0) {
  return { action, ownership: { state, trackId: id }, localTrackId: 99, badges: ['本地'], isrc: 'JPE301500361' } as Models.app.CandidateRow
}

describe('candidate action protocol', () => {
  it('keeps same-ISRC tracks independent in review and search', () => {
    const review = [row('skip', 'owned', 7), row('accept', 'unowned')]
    const search = [row('owned', 'owned', 7), row('add', 'unowned')]
    expect(review.map(candidateActionLabel)).toEqual(['跳过', '接受'])
    expect(search.map(candidateActionLabel)).toEqual(['已在库中', '加入曲库'])
    for (const candidate of [...review, ...search]) expect(candidateActionEnabled(candidate)).toBe(true)
    expect(review.map(candidateLocalTarget)).toEqual([7, 0])
  })

  it('never authorizes using localized labels, related IDs or inconsistent ownership', () => {
    for (const candidate of [row('合并', 'owned', 7), row('接受', 'unowned'), row('skip', 'unowned'), row('owned', 'owned'), row('add', 'owned', 7)]) {
      expect(candidateActionEnabled(candidate)).toBe(false)
    }
    expect(candidateActionLabel(row('合并', 'owned', 7))).toBe('不可操作')
  })

  it('keeps lookup errors and identity conflicts disabled', () => {
    for (const state of ['lookup_error', 'identity_conflict', '']) {
      for (const action of ['accept', 'skip', 'add', 'owned', 'unavailable']) {
        expect(candidateActionEnabled(row(action, state, 7))).toBe(false)
        expect(candidateLocalTarget(row(action, state, 7))).toBe(0)
      }
    }
    expect(candidateActionEnabled({ action: 'add' } as Models.app.CandidateRow)).toBe(false)
  })
})
