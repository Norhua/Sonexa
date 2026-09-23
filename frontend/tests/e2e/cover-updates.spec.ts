import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="900" height="900"><rect width="900" height="900" fill="tan"/></svg>' }))
})

test('same album rows and drawer update to newest revision without refreshing other albums', async ({ page }, info) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, list = api.ListTracks, detail = api.GetTrackDetail
    ;(window as any).__revision = 0
    api.ListTracks = async (...args: any[]) => {
      const result = await list(...args)
      return { ...result, tracks: result.tracks.map((t: any) => ({ ...t, releaseId: t.id <= 2 ? 20 : 21, coverRevision: t.id <= 2 ? (window as any).__revision : 0 })) }
    }
    api.GetTrackDetail = async (id: number) => {
      const d = await detail(id)
      return { ...d, track: { ...d.track, releaseId: id <= 2 ? 20 : 21, coverRevision: id <= 2 ? (window as any).__revision : 0 } }
    }
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  const rows = page.locator('.track-list [data-row-key]')
  await expect(rows.first().locator('img')).toHaveAttribute('src', /revision=0/)
  await rows.first().locator('td').last().dblclick()
  const other = await rows.nth(2).locator('img').getAttribute('src')
  await page.evaluate(() => {
    ;(window as any).__revision = 2
    ;(window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 2 })
  })
  for (const image of [rows.first().locator('img'), rows.nth(1).locator('img'), page.locator('aside img')]) {
    await expect(image).toHaveAttribute('src', /revision=2/)
  }
  await expect(rows.nth(2).locator('img')).toHaveAttribute('src', other!)
  await page.evaluate(() => (window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 1 }))
  await expect(rows.first().locator('img')).toHaveAttribute('src', /revision=2/)
  await page.screenshot({ path: info.outputPath('cover-shared-update.png') })
})

test('album header and rows refresh while late detail response cannot switch the album', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetAlbumDetail
    api.ListAlbums = async () => [20, 21].map(id => ({ releaseId: id, title: `专辑${id}`, coverRevision: 0, ownedCount: 2 }))
    api.GetAlbumDetail = async (id: number) => {
      const d = { ...await detail(id), releaseId: id, title: `专辑${id}`, coverRevision: (window as any).__albumRevision ?? 0 }
      if ((window as any).__holdAlbum && id === 20) return new Promise(resolve => { (window as any).__finishAlbum = () => resolve(d) })
      return d
    }
  })
  await page.locator('nav').getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /专辑20/ }).click()
  await page.evaluate(() => {
    ;(window as any).__albumRevision = 1
    ;(window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 1 })
  })
  await expect(page.locator('main img[src*="releaseId=20"]')).not.toHaveCount(0)
  for (const image of await page.locator('main img[src*="releaseId=20"]').all()) await expect(image).toHaveAttribute('src', /revision=1/)
  await page.evaluate(() => {
    ;(window as any).__holdAlbum = true
    ;(window as any).__albumRevision = 2
    ;(window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 2 })
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishAlbum)).toBe('function')
  await page.getByRole('button', { name: /专辑21/ }).click()
  await page.evaluate(() => (window as any).__finishAlbum())
  await expect(page.locator('main h3')).toHaveText('专辑21')
})

test('review candidates prefetch, follow only matching release events and cancel on exit', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, queue = api.GetReviewQueue
    api.GetReviewQueue = async () => {
      const q = await queue()
      q.items[0].candidates = [1, 2].map(id => ({ provider: 'musicbrainz', releaseRef: `release-${id}`, matchRef: `track-${id}`, title: `候选${id}`, badges: [], action: '接受', confidence: 80 }))
      return q
    }
    api.BuildCoverRefs = async (rows: any[]) => rows.map(row => ({ key: row.releaseRef, target: { provider: row.provider, ref: row.releaseRef }, usage: 'thumbnail' }))
    api.PrefetchCovers = async (refs: any[]) => { (window as any).__prefetched = refs }
    api.CancelCovers = async (keys: string[]) => { (window as any).__visibleCovers = keys }
  })
  await page.getByRole('button', { name: /复核队列/ }).click()
  const images = page.locator('main img')
  await expect(images).toHaveCount(2)
  await expect.poll(() => page.evaluate(() => (window as any).__prefetched?.length)).toBe(2)
  const other = await images.nth(1).getAttribute('src')
  await page.evaluate(() => (window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 1, provider: 'musicbrainz', ref: 'release-1' }))
  await expect(images.first()).toHaveAttribute('src', /revision=1/)
  await expect(images.nth(1)).toHaveAttribute('src', other!)
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__visibleCovers)).toEqual([])
})
