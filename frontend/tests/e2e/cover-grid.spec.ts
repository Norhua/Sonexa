import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

async function openSearch(page: Page) {
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  return page.getByRole('region', { name: '从网络搜索封面' })
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => { const d = await detail(id); return { ...d, track: { ...d.track, releaseId: 20 } } }
    api.StartCoverSearch = async (request: any) => {
      const source = request.sources[0]
      const card = (id: number, type: string, variants: any[]) => ({ source, sourceRef: 'album', image: { id, title: `${source} ${type}`, artist: '测试艺术家', primaryType: type, variants } })
      const cards = source === 'caa' ? [
        card(1, 'Front', [{ id: 12, spec: '1200' }, { id: 11, spec: '250' }, { id: 13, spec: 'original' }]),
        card(2, 'Back', [{ id: 21, spec: 'original' }]),
      ] : source === 'itunes' ? [card(3, 'Front', [{ id: 31, spec: '3000', constructed: true }, { id: 32, spec: '100', constructed: true }])]
        : source === 'qq' ? [card(4, 'Front', [{ id: 41, spec: '800', constructed: true }])] : []
      ;(window as any).runtime.EventsEmit('cover:results', { ...request, source, state: cards.length ? 'ok' : 'empty', cards, error: '' })
    }
    api.AdoptCoverCandidate = async (_id: string, variantId: number) => {
      ;(window as any).__adopted = variantId
      return { releaseId: 20, revision: 1, variantId, width: 600, height: 400 }
    }
  })
})

test('image12 badges, smallest-only previews, dimensions, separate images and explicit adoption', async ({ page }, info) => {
  const requests: number[] = []
  let release!: () => void
  const pending = new Promise<void>(resolve => { release = resolve })
  await page.route('**/cover?*', async route => {
    const id = Number(new URL(route.request().url()).searchParams.get('variantId'))
    if (id) { requests.push(id); await pending }
    await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="600" height="400"><rect width="600" height="400" fill="#a9598a"/><rect x="30" y="30" width="540" height="340" fill="#70496c"/></svg>' })
  })
  const view = await openSearch(page)
  await view.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(view.locator('article')).toHaveCount(4)
  const front = view.locator('article').filter({ hasText: 'caa Front' })
  const back = view.locator('article').filter({ hasText: 'caa Back' })
  const itunes = view.locator('article').filter({ hasText: 'itunes Front' })
  const qq = view.locator('article').filter({ hasText: 'qq Front' })
  await expect(back).toContainText('原图·尺寸未知')
  await expect(qq).toContainText('800px·待验证')
  await expect(itunes.locator('img')).toHaveAttribute('src', /variantId=32$/)
  await expect(front.locator('img')).toHaveAttribute('src', /variantId=11$/)
  await expect.poll(() => requests.length).toBe(4)
  expect([...requests].sort((a, b) => a - b)).toEqual([11, 21, 32, 41])
  await page.screenshot({ path: info.outputPath('cover-specs-pending.png') })
  release()
  await expect(qq.locator('span').filter({ hasText: /^600×400$/ })).toBeVisible()
  await expect(qq.locator('img')).toHaveCSS('object-fit', 'contain')
  const picture = (await qq.locator('button').first().boundingBox())!
  const badge = (await qq.locator('span').filter({ hasText: /^QQ$/ }).boundingBox())!
  expect(badge.x - picture.x).toBeLessThan(15)
  expect(picture.y + picture.height - badge.y - badge.height).toBeLessThan(15)
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await page.screenshot({ path: info.outputPath(`image12-grid-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
  await front.getByRole('button', { name: '1200px', exact: true }).click()
  await expect(front.locator('img')).toHaveAttribute('src', /variantId=12$/)
  await expect(front.locator('span').filter({ hasText: /^600×400$/ })).toBeVisible()
  await front.getByRole('button', { name: '查看大图', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '封面预览', exact: true })
  await expect(dialog.getByAltText('封面大图')).toHaveCSS('object-fit', 'contain')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(view).toBeVisible()
  await expect(view.locator('footer')).toContainText('CAA / Front / 600×400')
  expect(await page.evaluate(() => (window as any).__adopted)).toBeUndefined()
  await view.getByRole('button', { name: '应用到专辑', exact: true }).click()
  await expect(view.getByRole('status')).toContainText('600×400')
  expect(await page.evaluate(() => (window as any).__adopted)).toBe(12)
})

test('empty and failed searches differ, failed preview never shows broken image', async ({ page }, info) => {
  await page.route('**/cover?*', route => route.fulfill({ status: 503, body: '' }))
  const view = await openSearch(page)
  await view.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(view.getByText('预览失败，请尝试其他规格或重试来源')).toHaveCount(4)
  for (const image of await view.locator('article img').all()) await expect(image).toHaveCSS('opacity', '0')
  await page.screenshot({ path: info.outputPath('cover-preview-error.png') })
  for (const state of ['error', 'empty']) {
    await page.evaluate(state => {
      ;(window as any).go.app.App.StartCoverSearch = async (request: any) => {
        ;(window as any).runtime.EventsEmit('cover:results', { ...request, source: request.sources[0], state, cards: [], error: state === 'error' ? '来源不可用' : '' })
      }
    }, state)
    await view.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(view.getByText(state === 'error' ? '搜索失败，请重试来源' : '未找到图片', { exact: true })).toBeVisible()
    await page.screenshot({ path: info.outputPath(`cover-search-${state}.png`) })
  }
})
