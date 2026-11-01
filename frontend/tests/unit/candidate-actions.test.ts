import { describe, expect, it } from 'vitest'
import type { Models } from '../../src/api'
import { app, matching } from '../../wailsjs/go/models'
import { candidateActionEnabled, candidateActionLabel, candidateLocalTarget, invalidateCandidateOwnership } from '../../src/lib/candidateActions'

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

  it('invalidates stale ownership using Wails models without mutating candidate facts', () => {
    const original = new app.CandidateRow({
      ...row('owned', 'owned', 7), provider: 'musicbrainz', matchRef: 'track-a', releaseRef: 'release-a',
      title: 'you', confidence: 95, releaseId: 20, coverRevision: 3,
      ownership: { state: 'owned', trackId: 7, revision: 2, releaseRevision: 3, identity: { state: 'exact', track: { provider: 'musicbrainz', ref: 'track-a' }, release: { provider: 'musicbrainz', ref: 'release-a' } } },
    })
    const before = JSON.stringify(original)
    const [invalid] = invalidateCandidateOwnership([original], '刷新失败')
    expect(invalid).toBeInstanceOf(app.CandidateRow)
    expect(invalid.ownership).toBeInstanceOf(matching.LocalMatch)
    expect(invalid).toMatchObject({ title: 'you', matchRef: 'track-a', releaseRef: 'release-a', confidence: 95, action: 'unavailable', releaseId: 0, coverRevision: 0, coverError: '刷新失败' })
    expect(invalid.ownership.identity).toEqual(original.ownership.identity)
    expect(candidateActionEnabled(invalid)).toBe(false)
    expect(candidateLocalTarget(invalid)).toBe(0)
    expect(JSON.stringify(original)).toBe(before)
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
