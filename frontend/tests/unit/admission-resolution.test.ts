import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { app } from '../../wailsjs/go/models'

const api = vi.hoisted(() => ({
  CommitAdmission: vi.fn(), ConfirmReleaseAdditions: vi.fn(), ReplanAdmissionGroup: vi.fn(),
}))
vi.mock('@/api', () => ({ App: api }))
import { useAdmissionStore } from '../../src/stores/admission'

beforeEach(() => { setActivePinia(createPinia()); vi.clearAllMocks() })

it('returns actual position alternatives without committing an empty admission plan', async () => {
  const store = useAdmissionStore()
  const alternatives = app.AdmissionPlanView.createFrom({
    id: '', hash: '', resolution: { state: 'ambiguous', reason: 'multiple_track_positions', candidates: [
      { provider: 'musicbrainz', matchRef: 'first', title: 'you', discNumber: 1, trackNumber: 1 },
      { provider: 'musicbrainz', matchRef: 'second', title: 'you', discNumber: 2, trackNumber: 1 },
    ] },
  })
  expect(await store.run(async () => alternatives)).toBeNull()
  expect(api.CommitAdmission).not.toHaveBeenCalled()
  expect(store.plan).toBeNull()
  expect(store.busy).toBe(false)
  expect(store.resolution?.candidates.map(c => c.matchRef)).toEqual(['first', 'second'])
  api.CommitAdmission.mockResolvedValue({ trackId: 9 })
  const exact = app.AdmissionPlanView.createFrom({ id: 'chosen-second', hash: 'hash', baseline: { conflict: false, requiresConfirmation: false } })
  expect(await store.run(async () => exact)).toEqual({ trackId: 9 })
  expect(store.resolution).toBeNull()
  expect(api.CommitAdmission).toHaveBeenCalledWith('chosen-second', 'hash', {})
})
