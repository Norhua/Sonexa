import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'
const rows = (page: Page) => page.locator('.track-list [data-row-key]')
const options = (page: Page) => page.getByRole('button', { name: '视图选项', exact: true })
async function pickTag(page: Page, name: string) {
  await options(page).click()
  await page.getByRole('menuitem', { name: '按标签', exact: true }).click()
  await page.getByRole('menuitemradio', { name, exact: true }).click()
}
async function showAddedTime(page: Page) {
  await page.locator('.track-list thead th').first().click({ button: 'right' })
  await expect(page.getByRole('menuitemcheckbox', { name: '加入时间', exact: true })).toHaveAttribute('aria-checked', 'false')
  await page.getByRole('menuitemcheckbox', { name: '加入时间', exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    localStorage.setItem('sonexa.theme', 'light')
    const api = (window as any).go.app.App
    const list = api.ListTracks, collections = api.ListCollections, album = api.GetAlbumDetail
    api.ListTracks = async (filter: any) => {
      ;(window as any).__lastFilter = filter
      const result = await list(filter)
      const tracks = result.tracks.filter((track: any) => !filter.tagId ||
        (filter.tagId === 21 ? [1, 3].includes(track.id) : track.id === 2)).map((track: any) => ({
        ...track, addedAt: `2026-09-18T0${8 - track.id}:04:05Z`,
      }))
      return { tracks, total: tracks.length }
    }
    api.ListCollections = async (kind: string) => kind === 'tag' ? [
      { id: 21, name: '收藏', kind: 'tag', trackCount: 2 },
      { id: 22, name: '待听', kind: 'tag', trackCount: 1 },
    ] : collections(kind)
    api.ListArtists = async () => [{ id: 30, name: 'Artist A', trackCount: 7 }, { id: 31, name: 'Artist B', trackCount: 1 }]
    api.ListArtistsForTag = async (id: number) => [{ id: id === 21 ? 30 : 31, name: id === 21 ? 'Artist A' : 'Artist B', trackCount: 7 }]
    api.GetArtistHeader = async () => ({ facets: [{ role: '', label: '全部', count: 7 }, { role: 'vocal', label: '演唱', count: 2 }] })
    api.ListAlbumsForTag = async () => api.ListAlbums()
    api.GetAlbumDetail = async () => {
      const result = await album()
      result.rows = result.rows.map((row: any) => ({ ...row, addedAt: row.ghost ? null : '2026-09-18T07:04:05Z' }))
      return result
    }
  })
  await page.goto('/')
  await expect(rows(page)).toHaveCount(7)
})

test('view options has only tag filters, accessible checked state and composable search scope', async ({ page }, testInfo) => {
  await options(page).click()
  const tagMenu = page.getByRole('menuitem', { name: '按标签', exact: true })
  await expect(tagMenu).toHaveAttribute('aria-haspopup', 'menu')
  await expect(tagMenu).toHaveAttribute('aria-expanded', 'false')
  await expect(tagMenu.locator('svg.lucide-chevron-right')).toHaveCount(1)
  await expect(page.getByRole('menuitemradio')).toHaveCount(0)
  await tagMenu.click()
  await expect(tagMenu).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('menuitemradio', { name: '全部标签', exact: true })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByText('默认顺序', { exact: true })).toHaveCount(0)
  await expect(options(page).locator('svg.lucide-list-filter')).toHaveCount(1)
  await expect(options(page)).toHaveAttribute('aria-expanded', 'true')
  const menu = page.locator('[data-pc-name="tieredmenu"]')
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await expect.poll(() => menu.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dark ? 'rgb(23, 23, 23)' : 'rgb(255, 255, 255)')
    expect(await menu.evaluate(el => getComputedStyle(el).borderTopWidth)).toBe('1px')
    expect(await menu.locator('a').first().evaluate(el => getComputedStyle(el).display)).toBe('flex')
    expect(await menu.locator('a').first().evaluate(el => getComputedStyle(el).paddingLeft)).toBe('12px')
    const bounds = (await menu.boundingBox())!
    expect(bounds.x + bounds.width).toBeLessThanOrEqual(1440)
    const submenu = menu.locator('[data-pc-section="submenu"]')
    const childBounds = (await submenu.boundingBox())!, parentBounds = (await tagMenu.boundingBox())!
    expect(childBounds.x).toBeGreaterThanOrEqual(0)
    expect(childBounds.x + childBounds.width).toBeLessThanOrEqual(parentBounds.x + 1)
    expect(await submenu.evaluate(el => getComputedStyle(el).backgroundColor)).toBe(dark ? 'rgb(23, 23, 23)' : 'rgb(255, 255, 255)')
    await expect.poll(() => submenu.locator('a').first().evaluate(el => getComputedStyle(el).color)).toBe(dark ? 'rgb(255, 255, 255)' : 'rgb(64, 64, 64)')
    await page.screenshot({ path: testInfo.outputPath(`view-options-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
  await page.keyboard.press('Escape')
  await expect(options(page)).toHaveAttribute('aria-expanded', 'false')
  await expect(options(page)).toBeFocused()
  await pickTag(page, '收藏')
  await expect(rows(page)).toHaveCount(2)
  await expect(page.getByLabel('当前筛选')).toContainText('标签=收藏')
  await page.getByRole('searchbox').fill('Tetra')
  await page.getByRole('searchbox').press('Enter')
  await expect(rows(page)).toHaveCount(1)
  await expect(page.getByLabel('当前筛选')).toContainText('标签=收藏')
  await expect(page.getByLabel('当前筛选')).toContainText('搜索="Tetra"')
  await page.getByLabel('当前筛选').locator('[data-pc-name="chip"]').filter({ hasText: '搜索=' }).getByRole('button', { name: '移除筛选' }).click()
  await expect(page.getByRole('searchbox')).toHaveValue('')
  await expect(rows(page)).toHaveCount(2)
  await page.getByRole('searchbox').fill('Tetra')
  await page.getByRole('searchbox').press('Enter')
  await expect(rows(page)).toHaveCount(1)
  await pickTag(page, '全部标签')
  await expect(rows(page)).toHaveCount(1)
  await expect(page.getByLabel('当前筛选')).not.toContainText('标签=')
  await page.getByRole('button', { name: /测试歌单/ }).click()
  await pickTag(page, '待听')
  await expect(rows(page)).toHaveCount(1)
  expect(await page.evaluate(() => (window as any).__lastFilter)).toMatchObject({ collectionId: 10, tagId: 22 })
  await options(page).click()
  await page.getByRole('menuitem', { name: '按标签', exact: true }).click()
  await expect(page.getByRole('menuitemradio', { name: '待听', exact: true })).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await expect(page.getByRole('menuitemradio')).toHaveCount(0)
  await expect(rows(page)).toHaveCount(7)
  await pickTag(page, '收藏')
  expect(await page.evaluate(() => (window as any).__lastFilter)).toMatchObject({ recentOnly: true, tagId: 21 })
})

test('tag filters artist/album lists and current tracks without losing browse context', async ({ page }) => {
  await page.getByRole('button', { name: '艺术家', exact: true }).click()
  await pickTag(page, '收藏')
  await expect(page.getByRole('button', { name: /Artist B/ })).toHaveCount(0)
  await page.getByRole('button', { name: /Artist A/ }).click()
  await expect(rows(page)).toHaveCount(2)
  await page.getByRole('button', { name: '演唱 2', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__lastFilter)).toMatchObject({ artistId: 30, tagId: 21, role: 'vocal' })
  await expect(page.getByLabel('当前筛选')).toContainText('标签=收藏')
  await expect(page.getByLabel('当前筛选')).toContainText('角色=vocal')
  await page.getByLabel('当前筛选').locator('[data-pc-name="chip"]').filter({ hasText: '角色=' }).getByRole('button', { name: '移除筛选' }).click()
  expect(await page.evaluate(() => (window as any).__lastFilter)).toMatchObject({ artistId: 30, tagId: 21 })
  expect(await page.evaluate(() => (window as any).__lastFilter.role)).toBeUndefined()
  await expect(page.getByRole('button', { name: '全部 7', exact: true })).toHaveClass(/bg-highlight/)
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await pickTag(page, '收藏')
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first()).toHaveAttribute('data-ghost', 'false')
  expect(await page.evaluate(() => (window as any).__lastFilter)).toMatchObject({ releaseId: 20, tagId: 21 })
  await pickTag(page, '全部标签')
  await expect(rows(page)).toHaveCount(5)
  await page.getByRole('button', { name: '仅显示已有', exact: true }).click()
  await expect(rows(page)).toHaveCount(2)
})

test('tag menu empty state, keyboard selection and stale list responses', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.ListArtistsForTag
    api.ListArtistsForTag = (id: number) => id === 21 ? new Promise(resolve => {
      ;(window as any).__resolveArtists = async () => resolve(await original(id))
    }) : original(id)
  })
  await page.getByRole('button', { name: '艺术家', exact: true }).click()
  await pickTag(page, '收藏')
  await pickTag(page, '待听')
  await expect(page.getByRole('button', { name: /Artist B/ })).toBeVisible()
  await page.evaluate(() => (window as any).__resolveArtists())
  await expect(page.getByRole('button', { name: /Artist A/ })).toHaveCount(0)
  await page.mouse.move(0, 0) // 避免展开后静止在旧子菜单位置的指针抢走键盘高亮
  await options(page).focus()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-pc-section="rootlist"]')).toBeFocused()
  await page.keyboard.press('Home')
  await page.keyboard.press('ArrowRight')
  await expect(page.getByRole('menuitemradio', { name: '全部标签', exact: true })).toBeVisible()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('button', { name: /Artist A/ })).toBeVisible()
  await page.evaluate(() => {
    ;(window as any).go.app.App.ListCollections = async () => []
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  await expect(page.locator('nav').getByText('还没有标签', { exact: true })).toBeVisible()
  await options(page).click()
  await page.getByRole('menuitem', { name: '按标签', exact: true }).click()
  await expect(page.getByRole('menuitem', { name: '还没有标签', exact: true })).toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('searchbox').click()
  await expect(page.getByRole('menuitemradio')).toHaveCount(0)
  await page.evaluate(() => {
    ;(window as any).go.app.App.ListTracks = async () => { throw new Error('标签筛选读取失败') }
  })
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect(page.getByText('Error: 标签筛选读取失败', { exact: true })).toBeVisible()
})

test('tag submenu supports keyboard return, right-side placement and scrolling many tags', async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 1340, height: 733 })
  await rows(page).first().locator('td').last().dblclick()
  await page.mouse.move(0, 0)
  await options(page).focus()
  await page.keyboard.press('Enter')
  const root = page.locator('[data-pc-section="rootlist"]')
  const parent = page.getByRole('menuitem', { name: '按标签', exact: true })
  await expect(root).toBeFocused()
  await page.keyboard.press('Home')
  await page.keyboard.press('ArrowRight')
  await expect(parent).toHaveAttribute('aria-expanded', 'true')
  const submenu = page.locator('[data-pc-name="tieredmenu"] [data-pc-section="submenu"]')
  const childBounds = (await submenu.boundingBox())!, parentBounds = (await parent.boundingBox())!
  expect(childBounds.x).toBeGreaterThanOrEqual(parentBounds.x + parentBounds.width - 1)
  expect(childBounds.x + childBounds.width).toBeLessThanOrEqual(1340)
  await page.screenshot({ path: testInfo.outputPath('tag-submenu-right-with-drawer.png') })
  await page.keyboard.press('ArrowLeft')
  await expect(parent).toHaveAttribute('aria-expanded', 'false')
  await expect(options(page)).toHaveAttribute('aria-expanded', 'true')
  await page.keyboard.press('Enter')
  await expect(parent).toHaveAttribute('aria-expanded', 'true')
  await page.keyboard.press('Escape')
  await expect(options(page)).toBeFocused()
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.ListCollections = async (kind: string) => kind === 'tag' ? Array.from({ length: 60 }, (_, i) => ({
      id: i + 100, kind: 'tag', name: `长标签 ${i + 1} ${'LongName'.repeat(5)}`, trackCount: 1,
    })) : []
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  await expect(page.locator('nav').getByRole('button', { name: /长标签 60 / })).toHaveCount(1)
  await options(page).click()
  await parent.click()
  expect(await submenu.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
  const bounds = (await submenu.boundingBox())!
  expect(bounds.y).toBeGreaterThanOrEqual(0)
  expect(bounds.y + bounds.height).toBeLessThanOrEqual(733)
  expect(await submenu.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await page.mouse.move(0, 0)
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('End')
  await page.keyboard.press('Enter')
  await expect(options(page)).toHaveAttribute('aria-expanded', 'false')
  await expect(page.getByLabel('当前筛选')).toContainText('标签=长标签 60')
})

test('added time is hidden by default, survives reload, sorts and reaches local search and album rows', async ({ page }) => {
  const added = page.getByRole('columnheader', { name: '加入时间', exact: true })
  await expect(added).toHaveCount(0)
  await showAddedTime(page)
  await expect(rows(page).first().locator('td').last()).toHaveText('2026-09-18')
  await added.click()
  await expect(rows(page).first()).toHaveAttribute('data-row-key', 'track:7')
  await added.click()
  await expect(rows(page).first()).toHaveAttribute('data-row-key', 'track:1')
  await page.reload()
  await expect(added).toBeVisible()
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('searchbox').fill('Tetra')
  await page.getByRole('searchbox').press('Enter')
  await expect(rows(page)).toHaveCount(1)
  await expect(rows(page).first().locator('td').last()).toHaveText('2026-09-18')
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(added).toHaveCount(0)
  await showAddedTime(page)
  await expect(added).not.toHaveAttribute('tabindex', '0')
  await expect(rows(page).first().locator('td').last()).toHaveText('2026-09-18')
  await expect(rows(page).nth(1).locator('td').last()).toHaveText('—')
  await expect(page.locator('.track-list thead th').first()).toHaveText('音轨号')
})

test('drawer aligns values within each section only and wraps long content', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, original = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => ({
      ...await original(id),
      participants: [
        { role: 'vocal', label: '演唱', artists: [{ artistId: 30, name: 'A very long artist name with several words' }] },
        { role: 'lyricist', label: '作词', artists: [{ artistId: 31, name: 'Writer' }] },
      ],
      metadata: { country: 'JP', label: 'VeryLongLabelWithoutAnySpaces'.repeat(3), catalogNumber: 'TEST-001', genre: 'A long genre description that should wrap rather than disappear' },
    })
    document.documentElement.classList.remove('dark')
  })
  await rows(page).first().locator('td').last().dblclick()
  const drawer = page.locator('aside')
  const starts = []
  for (const name of ['参与者', '发行信息', '厂牌与目录号']) {
    const section = drawer.locator('section').filter({ has: page.getByRole('heading', { name, exact: true }) })
    const xs = await section.locator('dd').evaluateAll(els => els.map(el => el.getBoundingClientRect().x))
    expect(new Set(xs).size).toBe(1)
    starts.push(xs[0])
  }
  expect(new Set(starts).size).toBe(3)
  const overflow = await drawer.locator('.detail-fields dd, .detail-fields button').evaluateAll(els =>
    els.filter(el => el.getBoundingClientRect().right > el.closest('aside')!.getBoundingClientRect().right - 16).length)
  expect(overflow).toBe(0)
  const scroll = drawer.locator('div.overflow-auto')
  expect(await scroll.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true)
  await expect.poll(() => drawer.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(250, 250, 250)')
  await page.screenshot({ path: testInfo.outputPath('section-alignment-light.png') })
})
