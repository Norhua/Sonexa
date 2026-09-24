import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test('iTunes drawer uses release detail cover and explains unavailable participant roles', async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="1200"><rect width="1200" height="1200" fill="navy"/></svg>' }))
  await page.goto('/')
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const d = await original(id)
      return {
        ...d,
        track: { ...d.track, title: 'ツバサ', artist: '4円', releaseId: 20 },
        metadata: { ...d.metadata, matchSource: 'itunes', year: 2019, genre: 'J-Pop', trackNumber: 2 },
        participants: [], creditText: '4円',
        creditSpans: [{ start: 0, end: 4, artistId: 5, text: '4円' }],
        coverProvider: 'itunes', coverRef: 'jp:1475065332',
      }
    }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const drawer = page.locator('aside').filter({ hasText: '参与者' })
  await expect(drawer.getByText('ツバサ', { exact: true })).toBeVisible()
  await expect(drawer.getByRole('button', { name: '4円', exact: true })).toBeVisible()
  await expect(drawer.getByText('iTunes 不提供演唱、作曲、作词等角色数据', { exact: false })).toBeVisible()
  await expect(drawer.locator('img')).toHaveAttribute('src', /usage=detail.*releaseId=20/)
  await expect(drawer.getByText('J-Pop', { exact: true })).toBeVisible()
  await expect(drawer.getByText('nil', { exact: true })).toHaveCount(0)
})
