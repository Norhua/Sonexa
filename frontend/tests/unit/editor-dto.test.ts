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

import { app } from '../../wailsjs/go/models'
it('constructs admission projections without generic snapshot conversion',()=>{
 const view=app.AdmissionPlanView.createFrom({id:'plan',hash:'hash',seed:{snapshot:{release:{identity:{provider:'itunes',ref:'cn:1'},title:{state:'value',value:'Album'},credit:{state:'value',value:{text:'Artist'}}}}},baseline:{resolution:{groups:[],evidence:[]}}})
 expect(view.seed.snapshot.release?.credit.value.text).toBe('Artist')
 expect('seal' in view).toBe(false)
})
