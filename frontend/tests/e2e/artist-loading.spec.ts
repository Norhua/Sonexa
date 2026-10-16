import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => { localStorage.setItem('sonexa.theme', 'light') })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => {
    document.documentElement.classList.remove('dark')
    const w = window as any, api = w.go.app.App
    api.ListArtists = async () => [{ id: 30, name: 'Artist A', trackCount: 1 }, { id: 31, name: 'Artist B', trackCount: 1 }]
    w.__headers = {}; w.__tracks = {}
    api.GetArtistHeader = (id: number) => new Promise(resolve => {
      w.__headers[id] = () => resolve({ title: `Artist ${id}`, facets: [{ role: '', label: `Facet ${id}`, count: 1 }] })
    })
    api.ListTracks = (filter: any) => filter.artistId ? new Promise(resolve => {
      w.__tracks[filter.artistId] = () => resolve({ tracks: [{ id: filter.artistId, title: `Track ${filter.artistId}` }], total: 1 })
    }) : Promise.resolve({ tracks: [{ id: 1, title: 'Library' }], total: 1 })
  })
})

test('artist table loading stays light and only the latest selection can update rows and facets', async ({ page }, testInfo) => {
  await page.getByRole('button', { name: '艺术家', exact: true }).click()
  await page.getByRole('button', { name: /Artist A/ }).click()
  await page.evaluate(() => (window as any).__headers[30]())
  const mask = page.locator('.track-list [data-pc-section="mask"]')
  await expect(mask).toBeVisible()
  expect(await mask.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(255, 255, 255)')
  expect(await mask.evaluate(el => getComputedStyle(el).backdropFilter)).toBe('none')
  await page.screenshot({ path: testInfo.outputPath('artist-loading-light.png') })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect.poll(() => mask.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(23, 23, 23)')
  await page.screenshot({ path: testInfo.outputPath('artist-loading-dark.png') })
  await page.evaluate(() => document.documentElement.classList.remove('dark'))
  await page.evaluate(() => (window as any).__tracks[30]())
  await expect(page.getByRole('cell', { name: 'Track 30', exact: true })).toBeVisible()
  // 从 A 切到 B 再回 A：两次头部/曲目请求都真实在途，而非重复点击同一项。
  await page.getByRole('button', { name: /Artist B/ }).click()
  await page.getByRole('button', { name: /Artist A/ }).click()
  await expect(page.getByRole('button', { name: /Artist A/ })).toHaveAttribute('aria-current', 'true')
  await expect(page.getByRole('cell', { name: 'Track 30', exact: true })).toHaveCount(0)
  await expect(page.locator('.track-list')).toHaveAttribute('aria-busy', 'true')
  await page.evaluate(() => { (window as any).__headers[30](); (window as any).__tracks[30]() })
  await expect(page.getByRole('cell', { name: 'Track 30', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Facet 30 1', exact: true })).toBeVisible()
  await page.evaluate(() => { (window as any).__headers[31](); (window as any).__tracks[31]() })
  await expect(page.getByRole('cell', { name: 'Track 31', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Facet 31 1', exact: true })).toHaveCount(0)
  await expect(mask).toHaveCount(0)
  // 在途请求不允许离开艺术家页后把导航抢回来。
  await page.getByRole('button', { name: /Artist B/ }).click()
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  await page.evaluate(() => { (window as any).__headers[31](); (window as any).__tracks[31]() })
  await expect(page.getByRole('heading', { name: '全部曲目', exact: true })).toBeVisible()
})

test('missing roles only show the empty role hint, while explicit roles remain clickable', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.GetTrackDetail = async (id: number) => ({
      track: { id, title: '綿雪の夢', artist: id === 3 ? null : 'ハトリアヤ' },
      creditText: id === 3 ? '' : 'ハトリアヤ',
      creditSpans: id === 3 ? [] : [{ start: 0, end: 15, text: 'ハトリアヤ', artistId: 30 }],
      participants: id === 2 ? [{ role: 'vocal', label: '演唱', artists: [{ artistId: 30, name: 'ハトリアヤ' }] }] : [], tags: [],
    })
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const participants = page.locator('aside section').filter({ has: page.getByRole('heading', { name: '参与者', exact: true }) })
  await expect(participants.getByText('署名', { exact: true })).toHaveCount(0)
  await expect(participants.getByRole('button', { name: 'ハトリアヤ', exact: true })).toHaveCount(0)
  await expect(participants.getByText('暂无参与角色信息', { exact: true })).toBeVisible()
  await expect(participants.getByText('演唱', { exact: true })).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('credit-without-roles.png') })
  await page.locator('.track-list [data-row-key]').nth(1).locator('td').last().click()
  await expect(participants.getByText('演唱', { exact: true })).toBeVisible()
  await expect(participants.getByText('署名', { exact: true })).toHaveCount(0)
  await expect(participants.getByText('暂无参与角色信息')).toHaveCount(0)
  await page.locator('.track-list [data-row-key]').nth(2).locator('td').last().click()
  await expect(participants.getByRole('button')).toHaveCount(0)
  await expect(participants.getByText('暂无参与角色信息')).toBeVisible()
  await page.locator('.track-list [data-row-key]').nth(1).locator('td').last().click()
  await participants.getByRole('button', { name: 'ハトリアヤ', exact: true }).click()
  // 抽屉立即导航；新挂载的 ArtistsView 读取当前艺术家的头部。
  await expect.poll(() => page.evaluate(() => typeof (window as any).__headers[30])).toBe('function')
  await page.evaluate(() => (window as any).__headers[30]())
  await expect(page.getByRole('heading', { name: '艺术家', exact: true })).toBeVisible()
})
