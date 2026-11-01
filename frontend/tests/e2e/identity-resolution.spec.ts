import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: synthetic candidate expands into positions and only explicit second position commits`, async ({ page }) => {
    await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
    await page.goto('/')
    await page.evaluate((scope) => {
      const w = window as any, api = w.go.app.App
      w.__identityPrepared = []; w.__identityCommitted = []; w.__identityDone = false
      const action = scope === 'review' ? 'accept' : 'add'
      const candidate = { provider: 'musicbrainz', matchRef: 'recording@release', releaseRef: 'release', title: 'you', album: '特典CD', badges: ['MB'], action, ownership: { state: 'unowned', trackId: 0 }, confidence: 90, localTrackId: 0 }
      const positions = [1, 2].map(disc => ({ ...candidate, matchRef: `real-track-${disc}`, discNumber: disc, trackNumber: 1, trackTotal: 2, note: `第 ${disc} 碟 · 第 1 轨` }))
      api.SearchUpstream = async () => ({ byProvider: [{ capabilities: [], rows: [candidate] }] })
      api.GetReviewQueue = async () => w.__identityDone
        ? { items: [], total: 0, pending: 0, failed: 0 }
        : { items: [{ id: 11, batchId: 1, status: 'review', inTitle: 'you', inArtist: '歌手', inAlbum: '特典CD', batchTag: 'identity.csv', candidates: [candidate], triedVariants: [] }], total: 1, pending: 1, failed: 0 }
      api.PrepareAdmission = async (row: any, itemId: number) => {
        w.__identityPrepared.push({ ref: row.matchRef, itemId })
        if (row.matchRef === candidate.matchRef) return { resolution: { state: 'ambiguous', reason: 'multiple_track_positions', candidates: positions } }
        return { id: row.matchRef, hash: 'hash', baseline: { conflict: false, requiresConfirmation: false } }
      }
      api.CommitAdmission = async (id: string) => { w.__identityCommitted.push(id); w.__identityDone = true; return { trackId: 90 } }
    }, scope)
    if (scope === 'review') {
      await page.getByRole('button', { name: /复核队列/ }).click()
    } else {
      await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
      await page.getByRole('combobox').click()
      await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
      await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
    }
    const action = scope === 'review' ? '接受' : '加入曲库'
    await page.getByRole('button', { name: action, exact: true }).click()
    await expect(page.getByText('第 2 碟 · 第 1 轨', { exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: action, exact: true })).toHaveCount(2)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(await page.evaluate(() => (window as any).__identityCommitted)).toEqual([])
    await page.getByRole('row').filter({ hasText: '第 2 碟 · 第 1 轨' }).getByRole('button', { name: action, exact: true }).click()
    await expect.poll(() => page.evaluate(() => (window as any).__identityCommitted)).toEqual(['real-track-2'])
    expect(await page.evaluate(() => (window as any).__identityPrepared)).toEqual([
      { ref: 'recording@release', itemId: scope === 'review' ? 11 : 0 },
      { ref: 'real-track-2', itemId: scope === 'review' ? 11 : 0 },
    ])
    if (scope === 'review') await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  })
}
