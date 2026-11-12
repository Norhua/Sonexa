import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { App } from '../../src/api'
import { useAdmissionStore } from '../../src/stores/admission'
import {
  admissionHeader, admissionMode, commitsDirectly, evidenceLabel, formatDuration, groupActions, providerPosition,
  providerReleases, rejectedText, reviewReasonText, typedWordMatches, type Plan,
} from '../../src/lib/admissionPlan'

vi.mock('../../src/api', () => ({ App: {
  CommitAdmission: vi.fn(), ConfirmReleaseAdditions: vi.fn(), ConfirmTypedAdmission: vi.fn(), ReplanAdmissionGroupChoice: vi.fn(),
  UseSongProvider: vi.fn(), RefreshAdmissionCoverage: vi.fn(),
} }))

const provider = (releaseId: number, extra: Record<string, unknown> = {}) => ({
  releaseId, identity: { provider: 'musicbrainz', ref: `rel-${releaseId}` }, title: `发行 ${releaseId}`, credit: '艺术家',
  discNumber: 1, trackNumber: 2, trackTitle: '歌曲', durationMs: 201000, trackRef: `t-${releaseId}`, recordingRef: 'rec',
  evidence: 'recording', trackRefs: 3, trackTotal: 12, ...extra,
})
const plan = (baseline: Record<string, unknown> = {}, seed: Record<string, unknown> = {}, id = 'plan-1'): Plan => ({
  id, hash: 'hash', expiresAt: '',
  seed: { snapshot: { release: { identity: { provider: 'musicbrainz', ref: 'rel-b' }, title: { state: 'value', value: '发行 B' }, credit: { state: 'value', value: { text: '艺术家' } } } }, ...seed },
  baseline: { coverage: 'not_applicable', needsTypedConfirm: false, requiresConfirmation: false, conflict: false, resolution: { groups: [], evidence: [] }, ...baseline },
}) as unknown as Plan

beforeEach(() => { setActivePinia(createPinia()); vi.resetAllMocks() })

describe('dialog rules', () => {
  it('classifies the plan and picks the header', () => {
    expect(admissionMode(plan().baseline)).toBe('none')
    expect(commitsDirectly(plan().baseline)).toBe(true)
    expect(admissionMode(plan({ requiresConfirmation: true }).baseline)).toBe('membership')
    expect(admissionMode(plan({ needsTypedConfirm: true, coverage: 'provided' }).baseline)).toBe('provided')
    expect(admissionMode(plan({ needsTypedConfirm: true, coverage: 'possible' }).baseline)).toBe('provided')
    expect(admissionMode(plan({ needsTypedConfirm: true, coverage: 'unknown' }).baseline)).toBe('unknown')
    // 组冲突优先：必须先选判组方式。
    expect(admissionMode(plan({ conflict: true, needsTypedConfirm: true, coverage: 'provided' }).baseline)).toBe('conflict')
    // 已有发行能提供、但目标就是主发行：后端不要求输入确认，前端不再弹这类提示。
    expect(commitsDirectly(plan({ coverage: 'provided', providers: [provider(1)] }).baseline)).toBe(true)
    expect(admissionHeader('provided')).toBe('这首歌可由已有发行提供')
    expect(admissionHeader('unknown')).toBe('无法确认已有发行是否包含这首歌')
    expect(admissionHeader('membership')).toBe('确认发行成员变化')
  })
  it('accepts only the exact confirmation word', () => {
    for (const ok of ['Yes', ' Yes ', 'Yes\n']) expect(typedWordMatches(ok)).toBe(true)
    for (const bad of ['yes', 'YES', 'Y', '', 'Yes.', 'Ye s']) expect(typedWordMatches(bad)).toBe(false)
  })
  it('groups provider entries by release, primary first, and marks unusable ones', () => {
    const releases = providerReleases(plan({ providers: [
      provider(1), provider(1, { trackNumber: 9 }), provider(2, { trackRef: '' }),
      provider(3, { identity: undefined, trackRef: '', trackId: 44, evidence: 'text' }),
    ] }).baseline)
    expect(releases.map(r => [r.releaseId, r.entries.length, r.usable])).toEqual([[1, 2, true], [2, 1, false], [3, 1, true]])
    expect(releases[2]).toMatchObject({ source: '本地', ref: '' })
    expect(providerReleases(plan().baseline)).toEqual([])
  })
  it('formats evidence, duration and position', () => {
    expect(evidenceLabel('recording')).toBe('录音相同')
    expect(evidenceLabel('text')).toBe('疑似：曲名与时长相同')
    expect(formatDuration(201000)).toBe('3:21')
    expect(formatDuration(59499)).toBe('0:59')
    expect(formatDuration(0)).toBe('')
    expect(formatDuration(undefined)).toBe('')
    expect(providerPosition(provider(1) as never)).toBe('1-2')
    expect(providerPosition(provider(1, { discNumber: 0, trackNumber: 0 }) as never)).toBe('')
  })
  it('explains rejected name evidence with the source of the proposed release, not a fixed upstream name', () => {
    expect(rejectedText({ groupId: 7, reason: 'native_mismatch' } as never, 'musicbrainz')).toBe('因 musicbrainz 的发行组不同，未并入专辑组 #7')
    expect(rejectedText({ groupId: 7, reason: 'native_mismatch' } as never, 'qq')).toContain('qq')
    expect(rejectedText({ groupId: 7, reason: 'separated' } as never, 'qq')).toBe('此前已判定不是同一组，未并入专辑组 #7')
  })
  it('offers group actions only where the backend accepts them', () => {
    const groups = [{ id: 1, members: [] }, { id: 2, members: [] }]
    const named = [{ groupId: 1, kind: 'release-current' }]
    expect(groupActions(plan({ conflict: true, resolution: { groups, evidence: named } }))).toEqual({ choose: true, separate: true })
    expect(groupActions(plan({ requiresConfirmation: true, resolution: { groups: [groups[0]], evidence: named } }))).toEqual({ choose: false, separate: true })
    // 只有上游组身份命中：不是名称判断，没有「保持独立」可言。
    expect(groupActions(plan({ requiresConfirmation: true, resolution: { groups: [groups[0]], evidence: [{ groupId: 1, kind: 'native' }] } })).separate).toBe(false)
    // 发行已在库、整发行换组、已经选了独立成组：都不再提供。
    expect(groupActions(plan({ conflict: true, existingReleaseId: 5, resolution: { groups, evidence: named } })).separate).toBe(false)
    expect(groupActions(plan({ conflict: true, resolution: { groups, evidence: named } }, { moveReleaseId: 5 })).separate).toBe(false)
    expect(groupActions(plan({ requiresConfirmation: true, resolution: { groups: [groups[0]], evidence: named } }, { keepSeparate: true })).separate).toBe(false)
    expect(groupActions(plan())).toEqual({ choose: false, separate: false })
  })
  it('has copy for every review reason, including the new one', () => {
    expect(reviewReasonText('song_unresolved')).toBe('无法确认已有发行是否包含这首歌，需复核')
    expect(reviewReasonText('new_release')).toContain('新增发行')
    expect(reviewReasonText('something_else')).toBe('something_else')
    expect(reviewReasonText(null)).toBe('')
  })
})

describe('admission store', () => {
  const result = { trackId: 9 }
  it('commits directly when nothing needs confirming', async () => {
    vi.mocked(App.CommitAdmission).mockResolvedValue(result as never)
    const store = useAdmissionStore()
    expect(await store.run(async () => plan())).toEqual(result)
    expect(store.plan).toBeNull()
    expect(App.CommitAdmission).toHaveBeenCalledWith('plan-1', 'hash', {})
  })
  it('requires the typed word and sends it to the server for a typed approval', async () => {
    vi.mocked(App.ConfirmTypedAdmission).mockResolvedValue({ planId: 'plan-1', hash: 'hash', kind: 'typed', credential: 'c' } as never)
    vi.mocked(App.CommitAdmission).mockResolvedValue(result as never)
    const store = useAdmissionStore()
    const pending = store.run(async () => plan({ needsTypedConfirm: true, coverage: 'provided', providers: [provider(1)] }))
    await vi.waitFor(() => expect(store.plan).not.toBeNull())
    await store.confirm('yes')
    await store.confirm('')
    expect(App.ConfirmTypedAdmission).not.toHaveBeenCalled()
    expect(App.CommitAdmission).not.toHaveBeenCalled()
    await store.confirm(' Yes ')
    expect(App.ConfirmTypedAdmission).toHaveBeenCalledWith('plan-1', 'hash', ' Yes ')
    expect(App.ConfirmReleaseAdditions).not.toHaveBeenCalled()
    expect(vi.mocked(App.CommitAdmission).mock.calls[0]![2]).toMatchObject({ kind: 'typed', credential: 'c' })
    expect(await pending).toEqual(result)
  })
  it('uses the existing release and commits without any confirmation', async () => {
    vi.mocked(App.UseSongProvider).mockResolvedValue(plan({ coverage: 'provided', existingReleaseId: 1 }, {}, 'plan-2'))
    vi.mocked(App.CommitAdmission).mockResolvedValue(result as never)
    const store = useAdmissionStore()
    const pending = store.run(async () => plan({ needsTypedConfirm: true, coverage: 'provided', providers: [provider(1)] }))
    await vi.waitFor(() => expect(store.plan).not.toBeNull())
    await store.useProvider(1)
    expect(App.UseSongProvider).toHaveBeenCalledWith('plan-1', 'hash', 1)
    expect(App.CommitAdmission).toHaveBeenCalledWith('plan-2', 'hash', {})
    expect(App.ConfirmTypedAdmission).not.toHaveBeenCalled()
    expect(await pending).toEqual(result)
  })
  it('keeps the dialog open when the replanned release still needs confirmation', async () => {
    vi.mocked(App.UseSongProvider).mockResolvedValue(plan({ requiresConfirmation: true }, {}, 'plan-2'))
    const store = useAdmissionStore()
    void store.run(async () => plan({ needsTypedConfirm: true, coverage: 'provided', providers: [provider(1)] }))
    await vi.waitFor(() => expect(store.plan).not.toBeNull())
    await store.useProvider(1)
    expect(store.plan?.id).toBe('plan-2')
    expect(App.CommitAdmission).not.toHaveBeenCalled()
  })
  it('fetches tracklists once automatically when coverage is unknown', async () => {
    vi.mocked(App.RefreshAdmissionCoverage).mockResolvedValue(plan({ needsTypedConfirm: true, coverage: 'provided', providers: [provider(1)] }, {}, 'plan-2'))
    const store = useAdmissionStore()
    void store.run(async () => plan({ needsTypedConfirm: true, coverage: 'unknown', undecidable: [{ releaseId: 1, reason: 'tracklist_incomplete' }] }))
    await vi.waitFor(() => expect(store.plan?.id).toBe('plan-2'))
    expect(App.RefreshAdmissionCoverage).toHaveBeenCalledTimes(1)
    expect(store.fetching).toBe(false)
    expect(admissionMode(store.plan?.baseline)).toBe('provided')
  })
  it('keeps the plan and reports why when fetching tracklists fails', async () => {
    vi.mocked(App.RefreshAdmissionCoverage).mockRejectedValue(new Error('offline'))
    const store = useAdmissionStore()
    void store.run(async () => plan({ needsTypedConfirm: true, coverage: 'unknown', undecidable: [{ releaseId: 1, reason: 'tracklist_incomplete' }] }))
    await vi.waitFor(() => expect(store.coverageError).toContain('offline'))
    expect(store.plan?.id).toBe('plan-1')
    expect(store.error).toBe('')
    expect(store.busy).toBe(false)
  })
  it('sends the three group actions', async () => {
    vi.mocked(App.ReplanAdmissionGroupChoice).mockImplementation(async (_id, _hash, action) => plan({ requiresConfirmation: true }, {}, `after-${action}`))
    const store = useAdmissionStore()
    void store.run(async () => plan({ conflict: true }))
    await vi.waitFor(() => expect(store.plan).not.toBeNull())
    await store.merge(2)
    expect(App.ReplanAdmissionGroupChoice).toHaveBeenLastCalledWith('plan-1', 'hash', 'merge', 2)
    await store.join(3)
    expect(App.ReplanAdmissionGroupChoice).toHaveBeenLastCalledWith('after-merge', 'hash', 'join', 3)
    await store.separate()
    expect(App.ReplanAdmissionGroupChoice).toHaveBeenLastCalledWith('after-join', 'hash', 'separate', 0)
    expect(store.plan?.id).toBe('after-separate')
    expect(App.CommitAdmission).not.toHaveBeenCalled()
  })
  it('uses the ordinary approval for a membership-only plan', async () => {
    vi.mocked(App.ConfirmReleaseAdditions).mockResolvedValue({ planId: 'plan-1', hash: 'hash', kind: '', credential: 'm' } as never)
    vi.mocked(App.CommitAdmission).mockResolvedValue(result as never)
    const store = useAdmissionStore()
    const pending = store.run(async () => plan({ requiresConfirmation: true }))
    await vi.waitFor(() => expect(store.plan).not.toBeNull())
    await store.confirm()
    expect(App.ConfirmTypedAdmission).not.toHaveBeenCalled()
    expect(await pending).toEqual(result)
  })
})
