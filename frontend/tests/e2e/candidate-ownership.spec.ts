import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

const firstTrack = 'f0a33d1a-628a-4a53-bc4e-6af077505956'
const secondTrack = 'f4458acc-aa40-40b0-a0b7-aaa27df24b04'

async function setup(page: Page, scope: 'review' | 'search') {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(({ scope, firstTrack, secondTrack }) => {
    const w = window as any, api = w.go.app.App
    const state = w.__identityActions = { prepares: [] as any[], commits: [] as string[], owned: [firstTrack], done: false, searches: 0, refreshes: 0, failure: false, releaseIDs: { 'release-0': 20, 'release-1': 0 } as Record<string, number> }
    const candidates = [firstTrack, secondTrack].map((matchRef, i) => ({
      provider: 'musicbrainz', matchRef, releaseRef: `release-${i}`, title: 'you', artist: '歌手', album: i ? '歌曲合集' : '特典 CD',
      recordingRef: '2283ceda-78b1-4d20-8f52-bdf73bd53686', isrc: 'JPE301500361', confidence: 95, badges: ['musicbrainz'],
    }))
    const project = (row: any) => {
      const owned = state.owned.includes(row.matchRef), trackId = row.matchRef === firstTrack ? 7 : 90
      if (state.failure) return { ...row, action: 'unavailable', ownership: { state: 'lookup_error', trackId: 0 }, localTrackId: 0, badges: ['musicbrainz'], note: '本地查询失败', coverError: '本地查询失败', releaseId: 0 }
      return { ...row, releaseId: state.releaseIDs[row.releaseRef] ?? 0, coverRevision: 1, coverError: '', note: '', localTrackId: owned ? trackId : 0, ownership: { state: owned ? 'owned' : 'unowned', trackId: owned ? trackId : 0 },
        action: scope === 'review' ? (owned ? 'skip' : 'accept') : (owned ? 'owned' : 'add'), badges: owned ? ['本地', 'musicbrainz'] : ['musicbrainz'] }
    }
    api.SearchUpstream = async () => { state.searches++; return { byProvider: [{ rows: candidates.map(project), capabilities: [] }] } }
    api.RefreshSearchCandidates = async (rows: any[]) => { state.refreshes++; return rows.map(project) }
    api.RefreshReviewCandidates = async (rows: any[]) => { state.refreshes++; return rows.map(project) }
    w.__projectCandidate = project
    api.GetReviewQueue = async () => ({ items: state.done ? [] : [{ id: 11, revision: 4, status: 'review', inTitle: '原始 you', inArtist: '歌手', inAlbum: '歌曲合集', batchTag: 'identity.csv', candidates: candidates.map(project) }], total: state.done ? 0 : 1, pending: state.done ? 0 : 1, failed: 0 })
    api.PrepareAdmission = async (row: any, itemId: number) => { state.prepares.push({ action: 'accept', ref: row.matchRef, itemId }); return { id: row.matchRef, hash: 'hash', baseline: { requiresConfirmation: false, conflict: false } } }
    api.PrepareSkipExisting = async (row: any, itemId: number, revision: number) => { state.prepares.push({ action: 'skip', ref: row.matchRef, itemId, revision }); return { id: row.matchRef, hash: 'hash', baseline: { requiresConfirmation: false, conflict: false } } }
    api.CommitAdmission = async (id: string) => { state.commits.push(id); const existing = state.owned.includes(id); if (!existing) state.owned.push(id); state.done = true; return { trackId: existing ? 7 : 90, existing } }
  }, { scope, firstTrack, secondTrack })
  if (scope === 'review') {
    await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  } else {
    await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
    await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  }
}

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: same ISRC leaves the second album addable and submits its exact track`, async ({ page }) => {
    await setup(page, scope)
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    await expect(first.getByText('本地', { exact: true })).toBeVisible()
    await expect(second.getByText('本地', { exact: true })).toHaveCount(0)
    await expect(first.getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeVisible()
    await second.getByRole('button', { name: scope === 'review' ? '接受' : '加入曲库', exact: true }).click()
    await expect.poll(() => page.evaluate(() => (window as any).__identityActions.commits)).toEqual([secondTrack])
    expect(await page.evaluate(() => (window as any).__identityActions.prepares)).toEqual([{ action: 'accept', ref: secondTrack, itemId: scope === 'review' ? 11 : 0 }])
    if (scope === 'review') await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
    else await expect(second.getByRole('button', { name: '查看库中曲目：you', exact: true })).toBeVisible()
  })
}

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: library changes refresh actions and own covers without re-searching`, async ({ page }) => {
    await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="blue"/></svg>' }))
    await setup(page, scope)
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    const input = scope === 'review' ? page.getByLabel('曲名', { exact: true }) : page.getByPlaceholder('在 MusicBrainz 中搜索')
    await input.fill('保留未提交草稿')
    await expect(first.locator('img')).toHaveAttribute('src', /releaseId=20.*ref=release-0/)
    await expect(second.locator('img')).toHaveAttribute('src', /ref=release-1/)
    await expect(second.locator('img')).not.toHaveAttribute('src', /releaseId=20/)
    for (const owned of [[], [secondTrack], [firstTrack]]) {
      await page.evaluate(owned => { const w = window as any; w.__identityActions.owned = owned; w.__identityActions.releaseIDs = owned.length ? { 'release-0': 0, 'release-1': 21 } : {}; w.runtime.EventsEmit('library:changed') }, owned)
      const firstOwned = owned.includes(firstTrack), secondOwned = owned.includes(secondTrack)
      await expect(first.getByRole('button', { name: scope === 'review' ? (firstOwned ? '跳过' : '接受') : (firstOwned ? '查看库中曲目：you' : '加入曲库'), exact: true })).toBeEnabled()
      await expect(second.getByRole('button', { name: scope === 'review' ? (secondOwned ? '跳过' : '接受') : (secondOwned ? '查看库中曲目：you' : '加入曲库'), exact: true })).toBeEnabled()
      await expect(input).toHaveValue('保留未提交草稿')
      await expect(first.locator('img')).not.toHaveAttribute('src', /releaseId=21/)
    }
    await page.evaluate(() => { const w = window as any; w.__identityActions.failure = true; w.runtime.EventsEmit('library:changed') })
    await expect(page.getByRole('button', { name: '不可操作', exact: true })).toHaveCount(2)
    await expect(first.getByRole('button', { name: '不可操作', exact: true })).toBeDisabled()
    await expect(first.locator('img')).toHaveCount(0)
    await page.evaluate(() => { (window as any).__identityActions.failure = false })
    await page.getByRole('button', { name: '刷新本地状态', exact: true }).click()
    await expect(first.getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeEnabled()
    const state = await page.evaluate(() => (window as any).__identityActions)
    expect(state.searches).toBe(scope === 'search' ? 1 : 0)
    expect(state.commits).toEqual([])
  })
}

test('review: late local refresh cannot overwrite another item or a newer search', async ({ page }) => {
  await setup(page, 'review')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    api.RefreshReviewCandidates = async (rows: any[]) => {
      const snapshot = structuredClone(rows)
      return new Promise(resolve => { w.__finishLocal = () => resolve(snapshot) })
    }
    api.ResearchReviewItem = async () => [{ provider: 'musicbrainz', matchRef: 'new-query-target', releaseRef: 'new-release', title: '新查询结果', badges: ['musicbrainz'], action: 'accept', ownership: { state: 'unowned', trackId: 0 } }]
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLocal)).toBe('function')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect(page.getByText('新查询结果', { exact: true })).toBeVisible()
  await page.evaluate(() => (window as any).__finishLocal())
  await expect(page.getByText('新查询结果', { exact: true })).toBeVisible()
  await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
  await page.evaluate(() => (window as any).runtime.EventsEmit('library:changed'))
  await expect(page.getByText('新查询结果', { exact: true })).toHaveCount(0)
})

test('review: skip finishes the input instead of hiding a candidate', async ({ page }) => {
  await setup(page, 'review')
  await page.getByRole('row').filter({ hasText: '特典 CD' }).getByRole('button', { name: '跳过', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const state = await page.evaluate(() => (window as any).__identityActions)
  expect(state.prepares).toEqual([{ action: 'skip', ref: firstTrack, itemId: 11, revision: 4 }])
  expect(state.owned).toEqual([firstTrack])
  expect(state.commits).toEqual([firstTrack])
})
