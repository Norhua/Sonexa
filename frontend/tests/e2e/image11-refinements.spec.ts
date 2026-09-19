import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

const stub = fileURLToPath(new URL('./wails-stub.js', import.meta.url))
test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: stub })
  await page.addInitScript(() => localStorage.setItem('sonexa.theme', 'light'))
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
})

test('sidebar actions reveal only for their heading row, keep layout and support Tab activation', async ({ page, browserName }, testInfo) => {
  // macOS WebKit 的默认偏好仅 Tab 到文本输入；Option+Tab 才遍历所有控件。
  const nextControl = browserName === 'webkit' ? 'Alt+Tab' : 'Tab'
  const nav = page.locator('nav')
  const playlist = nav.getByRole('button', { name: '新建歌单', exact: true })
  const tag = nav.getByRole('button', { name: '新建标签', exact: true })
  const heading = nav.getByRole('heading', { name: '歌单', exact: true })
  await page.mouse.move(0, 0)
  await expect(playlist).toHaveCSS('opacity', '0')
  await expect(tag).toHaveCSS('opacity', '0')
  const original = await heading.boundingBox()
  await nav.getByRole('button', { name: /测试歌单/ }).hover()
  await expect(playlist).toHaveCSS('opacity', '0')
  await heading.hover()
  await expect(playlist).toHaveCSS('opacity', '1')
  await expect(tag).toHaveCSS('opacity', '0')
  expect(await heading.boundingBox()).toEqual(original)
  await page.screenshot({ path: testInfo.outputPath('sidebar-heading-hover.png') })
  await playlist.click()
  await expect(page.getByRole('dialog', { name: '新建歌单', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '新建歌单', exact: true })).toHaveCount(0)
  await nav.getByRole('heading', { name: '标签', exact: true }).hover()
  // 先移开键盘焦点；聚焦的按钮本来就应继续可见。
  await nav.getByRole('button', { name: '专辑', exact: true }).focus()
  await expect(playlist).toHaveCSS('opacity', '0')
  await expect(tag).toHaveCSS('opacity', '1')
  await page.mouse.move(0, 0)
  await expect(tag).toHaveCSS('opacity', '0')
  await page.keyboard.press(nextControl)
  await expect(playlist).toBeFocused()
  await expect(playlist).toHaveCSS('opacity', '1')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: '新建歌单', exact: true }).getByRole('textbox')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '新建歌单', exact: true })).toHaveCount(0)
  await nav.getByRole('button', { name: /测试歌单/ }).focus()
  await page.keyboard.press(nextControl)
  await expect(tag).toBeFocused()
  await expect(tag).toHaveCSS('opacity', '1')
  await page.keyboard.press('Enter')
  await expect(page.getByRole('dialog', { name: '新建标签', exact: true }).getByRole('textbox')).toBeFocused()
})

test('larger drawer cover and inline icon preserve cover search, errors and adoption', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1340, height: 733 })
  await page.route('**/cover?*', route => route.fulfill({
    contentType: 'image/svg+xml',
    body: '<svg xmlns="http://www.w3.org/2000/svg" width="500" height="500"><rect width="500" height="500" fill="#bd7546"/><circle cx="250" cy="220" r="150" fill="#f3dcb9"/><text x="250" y="435" text-anchor="middle" font-size="32" fill="white">Sonexa · cover fixture</text></svg>',
  }))
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    let adopted = ''
    api.GetTrackDetail = async (id: number) => ({
      ...await detail(id), coverProvider: 'musicbrainz', coverRef: 'test-release', adoptedCoverPath: adopted,
    })
    api.SearchCovers = async (...args: string[]) => {
      ;(window as any).__coverQuery = args
      throw new Error('封面查询失败')
    }
    api.AdoptCover = async (...args: any[]) => {
      ;(window as any).__adoption = args
      adopted = '/test/adopted-cover.png'
    }
  })
  const rows = page.locator('.track-list [data-row-key]')
  await rows.first().locator('td').last().dblclick()
  const drawer = page.locator('aside')
  const image = drawer.locator('img')
  const icon = drawer.getByRole('button', { name: '重新获取封面', exact: true })
  const title = drawer.getByRole('heading', { name: '綿雪の夢', exact: true })
  await expect(image).toHaveCSS('opacity', '1')
  await expect.poll(() => image.evaluate(el => (el as HTMLImageElement).naturalWidth)).toBe(500)
  const cover = (await image.boundingBox())!, bounds = (await drawer.boundingBox())!
  expect(bounds.width).toBe(340)
  expect(cover.width).toBe(280)
  expect(cover.height).toBe(280)
  expect(Math.abs(cover.x + cover.width / 2 - bounds.x - bounds.width / 2)).toBeLessThan(2)
  expect((await title.boundingBox())!.y - cover.y - cover.height).toBeLessThanOrEqual(20)
  const titleBlock = (await title.locator('..').boundingBox())!, button = (await icon.boundingBox())!
  expect(button.x).toBeGreaterThanOrEqual(titleBlock.x + titleBlock.width)
  expect(Math.abs(button.y + button.height / 2 - titleBlock.y - titleBlock.height / 2)).toBeLessThan(1)
  await expect(icon).toHaveText('')
  await expect(icon).toHaveAttribute('title', '重新获取封面')
  await expect(icon.locator('svg.lucide-image-plus')).toHaveCount(1)
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await expect.poll(() => drawer.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dark ? 'rgb(38, 38, 38)' : 'rgb(250, 250, 250)')
    await page.screenshot({ path: testInfo.outputPath(`drawer-inline-cover-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
  await icon.focus()
  await page.keyboard.press('Enter')
  await expect(drawer.getByText('Error: 封面查询失败', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__coverQuery)).toEqual(['綿雪の夢', 'ハトリアヤ', '狼と巫女とお菓子の家'])
  await page.evaluate(() => {
    ;(window as any).go.app.App.SearchCovers = async () => [{
      provider: 'itunes', sourceRef: 'test', title: '测试封面', constructed: true,
      variants: [{ url: 'https://example.test/600.jpg', label: '600px' }, { url: 'https://example.test/300.jpg', label: '300px' }],
    }]
  })
  await icon.click()
  await drawer.getByRole('button', { name: '600px', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__adoption)).toEqual([
    1, 'itunes', 'https://example.test/600.jpg', ['https://example.test/300.jpg'],
  ])
  await expect(drawer.getByRole('heading', { name: '选择封面', exact: true })).toHaveCount(0)
  await expect(image).toHaveAttribute('src', /adopted=/)
  // 长标题/署名可换行，不能把右侧图标挤到抽屉外。
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const result = await detail(id)
      return { ...result, track: { ...result.track, title: 'LongTitleWithoutSpaces'.repeat(5), artist: 'ArtistWithALongName'.repeat(5) } }
    }
  })
  await rows.nth(1).locator('td').last().click()
  await expect(drawer.locator('h2')).toHaveText('LongTitleWithoutSpaces'.repeat(5))
  expect((await icon.boundingBox())!.x + (await icon.boundingBox())!.width).toBeLessThanOrEqual(bounds.x + bounds.width - 16)
  expect(await drawer.locator('div.overflow-auto').evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
})

test('cover images start lazy requests while placeholders stay visible and failures keep distinct states', async ({ page }) => {
  let finishLoading!: () => void
  await page.route('**/cover?*', async route => {
    const ref = new URL(route.request().url()).searchParams.get('ref')
    if (ref === 'cover-1') {
      await new Promise<void>(resolve => { finishLoading = resolve })
      await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="250" height="250"><rect width="250" height="250" fill="tan"/></svg>' })
    } else {
      await route.fulfill({ status: ref === 'cover-2' ? 404 : 503, body: '' })
    }
  })
  await page.evaluate(() => {
    const api = (window as any).go.app.App, list = api.ListTracks
    api.ListTracks = async (...args: any[]) => {
      const result = await list(...args)
      return { ...result, tracks: result.tracks.map((track: any) => track.id <= 3 ? {
        ...track, matchSource: 'musicbrainz', releaseRef: `cover-${track.id}`,
      } : track) }
    }
    api.CoverOutcomes = async (sources: any[]) => sources.map(source => source.ref === 'cover-2' ? 'empty' : 'error')
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  const covers = page.locator('.track-list [data-row-key] td:first-child')
  await expect(covers.nth(0).locator('svg.lucide-loader-circle')).toBeVisible()
  await expect(covers.nth(0).locator('img')).toHaveCSS('opacity', '0')
  await expect.poll(() => typeof finishLoading).toBe('function')
  await expect(covers.nth(1).getByText('nil', { exact: true })).toBeVisible()
  await expect(covers.nth(2).locator('svg.lucide-image-off')).toBeVisible()
  await expect(covers.nth(3).locator('svg.lucide-image')).toBeVisible()
  for (const i of [1, 2]) await expect(covers.nth(i).locator('img')).toHaveCSS('opacity', '0')
  finishLoading()
  await expect(covers.nth(0).locator('img')).toHaveCSS('opacity', '1')
  await expect(covers.nth(0).locator('svg')).toHaveCount(0)
  expect((await covers.nth(0).locator('img').boundingBox())!.width).toBe(32)
})

test.describe('without hover input', () => {
  test.use({ hasTouch: true })
  test('sidebar creation entries remain available', async ({ page }) => {
    expect(await page.evaluate(() => matchMedia('(hover: hover)').matches)).toBe(false)
    for (const name of ['新建歌单', '新建标签']) {
      const button = page.getByRole('button', { name, exact: true })
      await expect(button).toHaveCSS('opacity', '1')
      await button.tap()
      await expect(page.getByRole('dialog', { name, exact: true })).toBeVisible()
      await page.keyboard.press('Escape')
    }
  })
})
