import { describe, expect, it } from 'vitest'
import { domain } from '../../wailsjs/go/models'

describe('generated editor DTO constructors', () => {
  it('preserves scalar, role and unknown values without invoking generic slice constructors', () => {
    const fields = {
      'track.year': { state: 'value', valueJson: '2012', mode: 'follow', source: 'musicbrainz' },
      'track.roles.vocal': { state: 'value', valueJson: JSON.stringify([{ artist: { name: '初音未来', identity: { provider: 'musicbrainz', ref: 'miku' } }, attributes: ['background vocals', 'guest'] }]), mode: 'user', source: 'user' },
      'track.lyrics': { state: 'unknown', valueJson: '' },
      'release.credit': { state: 'empty', valueJson: '' },
    }
    const payload = { trackId: 1, releaseId: 2, groupId: 3, current: fields, upstream: fields, snapshotJson: '{}', revisions: { 'track:1': 4 }, releaseTrackIds: [1, 2], otherReleaseTracks: 1, otherGroupTracks: 3, coverRevision: 2 }
    const session = domain.EditorSession.createFrom(JSON.stringify(payload))
    expect(JSON.parse(JSON.stringify(session))).toEqual(payload)
    expect(JSON.parse(session.current['track.roles.vocal']!.valueJson)[0].attributes).toEqual(['background vocals', 'guest'])
    const preview = domain.EditPreview.createFrom({ id: 'preview', fields, snapshotJson: '{}' })
    expect(preview.fields['track.lyrics']!.state).toBe('unknown')
    expect(preview.fields['release.credit']!.state).toBe('empty')
  })
})
