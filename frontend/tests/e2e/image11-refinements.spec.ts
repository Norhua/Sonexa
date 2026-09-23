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

test('drawer preserves cover layout and opens keyboard context menu without the old icon', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1340, height: 733 })
  await page.route('**/cover?*', route => route.fulfill({
    contentType: 'image/svg+xml',
    body: '<svg xmlns="http://www.w3.org/2000/svg" width="1200" height="1200"><rect width="1200" height="1200" fill="#bd7546"/></svg>',
  }))
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const value = await detail(id)
      return { ...value, track: { ...value.track, releaseId: 20 } }
    }
  })
  const rows = page.locator('.track-list [data-row-key]')
  await rows.first().locator('td').last().dblclick()
  const drawer = page.locator('aside'), image = drawer.locator('img')
  const title = drawer.getByRole('heading', { name: '綿雪の夢', exact: true })
  await expect(image).toHaveCSS('opacity', '1')
  await expect(image).toHaveAttribute('src', /usage=detail/)
  const cover = (await image.boundingBox())!, bounds = (await drawer.boundingBox())!
  expect(bounds.width).toBe(340)
  expect(cover.width).toBe(280)
  expect(cover.height).toBe(280)
  expect(Math.abs(cover.x + cover.width / 2 - bounds.x - bounds.width / 2)).toBeLessThan(2)
  expect((await title.boundingBox())!.y - cover.y - cover.height).toBeLessThanOrEqual(20)
  await expect(drawer.getByRole('button', { name: '重新获取封面', exact: true })).toHaveCount(0)
  const trigger = drawer.getByRole('button', { name: '更新歌曲图片', exact: true })
  await trigger.focus()
  await page.keyboard.press('Shift+F10')
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await expect(page.getByRole('menuitem', { name: '从本地存储中选择', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitem', { name: '从网络搜索', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await expect.poll(() => drawer.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dark ? 'rgb(38, 38, 38)' : 'rgb(250, 250, 250)')
    await page.screenshot({ path: testInfo.outputPath(`drawer-cover-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const value = await detail(id)
      return { ...value, track: { ...value.track, title: 'LongTitleWithoutSpaces'.repeat(5), artist: 'ArtistWithALongName'.repeat(5) } }
    }
  })
  await rows.nth(1).locator('td').last().click()
  await expect(drawer.locator('h2')).toHaveText('LongTitleWithoutSpaces'.repeat(5))
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
    api.CoverTargetOutcomes = async (sources: any[]) => sources.map(source => source.ref === 'cover-2' ? 'empty' : 'error')
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  const covers = page.locator('.track-list [data-row-key] td:first-child')
  await expect(covers.nth(0).locator('svg.lucide-loader-circle')).toBeVisible()
  await expect(covers.nth(0).locator('img')).toHaveCSS('opacity', '0')
  await expect.poll(() => typeof finishLoading).toBe('function')
  await expect(covers.nth(1).getByText('nil', { exact: true })).toBeVisible()
  await expect(covers.nth(2).locator('svg.lucide-image-off')).toBeVisible()
  await expect(covers.nth(3).getByText('nil', { exact: true })).toBeVisible()
  for (const i of [1, 2]) await expect(covers.nth(i).locator('img')).toHaveCSS('opacity', '0')
  finishLoading()
  await expect(covers.nth(0).locator('img')).toHaveCSS('opacity', '1')
  await expect(covers.nth(0).locator('svg')).toHaveCount(0)
  expect((await covers.nth(0).locator('img').boundingBox())!.width).toBe(32)
})

test('cover URL changes reset state and ignore late outcome responses', async ({ page }) => {
  await page.route('**/cover?*', async route => {
    const ref = new URL(route.request().url()).searchParams.get('ref')
    if (ref === 'fresh') {
      await route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="250" height="200"><rect width="250" height="200" fill="tan"/></svg>' })
    } else await route.fulfill({ status: 403, body: '' })
  })
  await page.evaluate(() => {
    const api = (window as any).go.app.App, list = api.ListTracks
    ;(window as any).__coverRef = 'disabled'
    api.ListTracks = async (...args: any[]) => {
      const result = await list(...args)
      return { ...result, tracks: result.tracks.map((track: any) => track.id === 1 ? {
        ...track, matchSource: 'musicbrainz', releaseRef: (window as any).__coverRef,
      } : track) }
    }
    api.CoverTargetOutcomes = async (queries: any[]) => {
      if (queries.some(q => q.ref === 'pending')) return new Promise(resolve => { (window as any).__finishOutcome = () => resolve(queries.map(() => 'empty')) })
      return queries.map(() => 'disabled')
    }
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  const cell = page.locator('.track-list [data-row-key]').first().locator('td').first()
  await expect(cell.getByText('出网已禁用', { exact: true })).toBeVisible()
  await page.evaluate(() => { (window as any).__coverRef = 'pending'; (window as any).runtime.EventsEmit('library:changed') })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishOutcome)).toBe('function')
  await page.evaluate(() => { (window as any).__coverRef = 'fresh'; (window as any).runtime.EventsEmit('library:changed') })
  await expect(cell.locator('img')).toHaveAttribute('src', /usage=thumbnail/)
  await expect(cell.locator('img')).toHaveAttribute('loading', 'lazy')
  await expect(cell.locator('img')).toHaveCSS('opacity', '1')
  await page.evaluate(() => (window as any).__finishOutcome())
  await expect(cell.getByText('nil', { exact: true })).toHaveCount(0)
  await expect(cell.locator('img')).toHaveCSS('opacity', '1')
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
