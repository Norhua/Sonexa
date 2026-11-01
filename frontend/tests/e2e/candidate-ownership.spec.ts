import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

const firstTrack = 'f0a33d1a-628a-4a53-bc4e-6af077505956'
const secondTrack = 'f4458acc-aa40-40b0-a0b7-aaa27df24b04'

async function setup(page: Page, scope: 'review' | 'search') {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(({ scope, firstTrack, secondTrack }) => {
    const w = window as any, api = w.go.app.App
    const state = w.__identityActions = { prepares: [] as any[], commits: [] as string[], owned: [firstTrack], done: false, searches: 0, refreshes: 0 }
    const candidates = [firstTrack, secondTrack].map((matchRef, i) => ({
      provider: 'musicbrainz', matchRef, releaseRef: `release-${i}`, title: 'you', artist: '歌手', album: i ? '歌曲合集' : '特典 CD',
      recordingRef: '2283ceda-78b1-4d20-8f52-bdf73bd53686', isrc: 'JPE301500361', confidence: 95, badges: ['musicbrainz'],
    }))
    const project = (row: any) => {
      const owned = state.owned.includes(row.matchRef), trackId = row.matchRef === firstTrack ? 7 : 90
      return { ...row, localTrackId: owned ? trackId : 0, ownership: { state: owned ? 'owned' : 'unowned', trackId: owned ? trackId : 0 },
        action: scope === 'review' ? (owned ? 'skip' : 'accept') : (owned ? 'owned' : 'add'), badges: owned ? ['本地', 'musicbrainz'] : ['musicbrainz'] }
    }
    api.SearchUpstream = async () => { state.searches++; return { byProvider: [{ rows: candidates.map(project), capabilities: [] }] } }
    api.RefreshSearchCandidates = async (rows: any[]) => { state.refreshes++; return rows.map(project) }
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

test('review: skip finishes the input instead of hiding a candidate', async ({ page }) => {
  await setup(page, 'review')
  await page.getByRole('row').filter({ hasText: '特典 CD' }).getByRole('button', { name: '跳过', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const state = await page.evaluate(() => (window as any).__identityActions)
  expect(state.prepares).toEqual([{ action: 'skip', ref: firstTrack, itemId: 11, revision: 4 }])
  expect(state.owned).toEqual([firstTrack])
  expect(state.commits).toEqual([firstTrack])
})
