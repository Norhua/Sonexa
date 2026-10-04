import { describe, expect, it } from 'vitest'
import { mergeAlbumRows, type AlbumTrackRow } from '../../src/lib/albumRows'
import { uniqueTracks } from '../../src/lib/trackRows'

const row = (releaseId: number, key: string, id = 0): AlbumTrackRow => ({ releaseId, provider: 'musicbrainz', id, ghost: !id, rowKey: `${releaseId}:${key}`, title: key, recordingRef: key, trackRef: `${releaseId}:${key}` })

describe('album group display', () => {
  it('unions two 13-track editions into 14 songs and prefers owned rows', () => {
    const common = Array.from({ length: 12 }, (_, i) => `common-${i}`)
    const rows = mergeAlbumRows([
      ...common.map(key => row(1, key)), row(1, 'exclusive A', 1),
      ...common.map((key, i) => row(2, key, i === 0 ? 3 : 0)), row(2, 'exclusive B', 2),
    ])
    expect(rows).toHaveLength(14)
    expect(rows.find(r => r.title === 'common-0')?.id).toBe(3)
    expect(rows.filter(r => !r.ghost).map(r => r.id).sort()).toEqual([1, 2, 3])
    expect(rows.find(r => r.title === 'exclusive B')?.releaseId).toBe(2)
  })

  it('keeps all local identities for selection, copy and deletion', () => {
    const rows = mergeAlbumRows([row(1, 'shared', 10), row(2, 'shared', 20)])
    expect(rows).toHaveLength(1)
    expect(uniqueTracks(rows).map(r => r.id)).toEqual([10, 20])
  })

  it('does not merge repeated recordings in one release or ambiguous matches', () => {
    const repeat = { ...row(1, 'same', 1), rowKey: 'repeat' }
    expect(mergeAlbumRows([row(1, 'same'), repeat, row(2, 'same')])).toHaveLength(3)
  })

  it('uses nonempty text and duration across sources without merging known distinct recordings', () => {
    const a = { ...row(1, 'first'), artist: ' Artist ', durationMs: 200000 }
    const b = { ...row(2, 'second'), provider: 'qq', title: 'FIRST', artist: 'artist', durationMs: 202000 }
    expect(mergeAlbumRows([a, b])).toHaveLength(1)
    expect(mergeAlbumRows([a, { ...b, provider: a.provider }])).toHaveLength(2)
    expect(mergeAlbumRows([a, { ...b, durationMs: 204000 }])).toHaveLength(2)
    expect(mergeAlbumRows([{ ...a, title: '' }, { ...b, title: '' }])).toHaveLength(2)
    expect(mergeAlbumRows([{ ...a, artist: '' }, { ...b, artist: '' }])).toHaveLength(2)
  })
})
