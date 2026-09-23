import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'
const rows = (page: Page) => page.locator('.track-list [data-row-key]')
const drawer = (page: Page) => page.locator('aside')
const cell = (page: Page, i: number) => rows(page).nth(i).locator('td').last()
const headerMenu = (page: Page) => page.getByRole('menuitemcheckbox')
async function openColumns(page: Page) {
  await page.locator('.track-list thead th').first().click({ button: 'right' })
  await expect(headerMenu(page)).toHaveCount(10)
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(rows(page)).toHaveCount(7)
})

test('open drawer follows plain single clicks, not multiselect or text drags; closing restores double click', async ({ page }) => {
  await cell(page, 0).click()
  await expect(drawer(page)).toHaveCount(0)
  await cell(page, 0).dblclick()
  await expect(drawer(page).getByRole('heading', { name: '綿雪の夢', exact: true })).toBeVisible()
  await cell(page, 1).click()
  await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  for (const modifiers of [['Meta'], ['Shift']] as ('Meta' | 'Shift')[][]) {
    await cell(page, 2).click({ modifiers })
    await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  }
  const box = (await rows(page).nth(4).locator('[data-text-cell]').first().boundingBox())!
  await page.mouse.move(box.x + 2, box.y + box.height / 2)
  await page.mouse.down()
  await page.mouse.move(box.x + 70, box.y + box.height / 2, { steps: 12 })
  await page.mouse.up()
  await expect.poll(() => page.evaluate(() => window.getSelection()?.toString().length ?? 0)).toBeGreaterThan(1)
  await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  await drawer(page).getByTitle('关闭', { exact: true }).click()
  await cell(page, 2).click()
  await expect(drawer(page)).toHaveCount(0)
  await cell(page, 2).dblclick()
  await expect(drawer(page).getByRole('heading', { name: 'Tetra.', exact: true })).toBeVisible()
})

test('late detail responses cannot override latest selection or reopen a closed drawer', async ({ page }) => {
  await cell(page, 0).dblclick()
  await expect(drawer(page)).toBeVisible()
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.GetTrackDetail
    api.GetTrackDetail = (id: number) => id === 2 ? new Promise(resolve => {
      ;(window as any).__resolveDetail = async () => resolve(await original(id))
    }) : original(id)
  })
  await cell(page, 1).click()
  await cell(page, 2).click()
  await expect(drawer(page).getByRole('heading', { name: 'Tetra.', exact: true })).toBeVisible()
  await page.evaluate(() => (window as any).__resolveDetail())
  await expect(drawer(page).getByRole('heading', { name: 'Tetra.', exact: true })).toBeVisible()
  await cell(page, 1).click()
  await drawer(page).getByTitle('关闭', { exact: true }).click()
  await page.evaluate(() => (window as any).__resolveDetail())
  await expect(drawer(page)).toHaveCount(0)
})

test('header menu toggles only existing columns, persists across views/reload and leaves selection/sort intact', async ({ page }, testInfo) => {
  await expect(page.getByRole('columnheader', { name: '#', exact: true })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: '状态', exact: true })).toHaveCount(0)
  await expect(page.locator('.track-list thead th')).toHaveCount(8)
  await expect(page.locator('.track-list thead th').first()).toHaveText('封面')
  await cell(page, 0).click()
  await page.getByRole('columnheader', { name: '标题', exact: true }).click()
  await openColumns(page)
  await expect(headerMenu(page).filter({ hasText: '艺术家' })).toHaveAttribute('aria-checked', 'true')
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    // 等待 transition-colors 完成，不能把切主题的一帧混色当成浅色截图。
    await expect.poll(() => headerMenu(page).first().locator('a').evaluate(el => getComputedStyle(el).color))
      .toBe(dark ? 'rgb(255, 255, 255)' : 'rgb(64, 64, 64)')
    await page.screenshot({ path: testInfo.outputPath(`columns-${dark ? 'dark' : 'light'}.png`) })
  }
  await headerMenu(page).filter({ hasText: '艺术家' }).click()
  await expect(page.getByRole('columnheader', { name: '艺术家', exact: true })).toHaveCount(0)
  await expect(page.locator('.track-list [aria-selected="true"]')).toHaveCount(1)
  await expect(page.getByRole('columnheader', { name: '标题', exact: true })).toHaveAttribute('aria-sort', 'ascending')
  await openColumns(page)
  await expect(headerMenu(page).filter({ hasText: '艺术家' })).toHaveAttribute('aria-checked', 'false')
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await expect(page.getByRole('columnheader', { name: '艺术家', exact: true })).toHaveCount(0)
  await page.reload()
  await expect(rows(page)).toHaveCount(7)
  await expect(page.getByRole('columnheader', { name: '艺术家', exact: true })).toHaveCount(0)
  await openColumns(page)
  await headerMenu(page).filter({ hasText: '艺术家' }).click()
  await expect(page.getByRole('columnheader', { name: '艺术家', exact: true })).toBeVisible()
  await page.getByRole('columnheader', { name: '标题', exact: true }).focus()
  await page.keyboard.press('Shift+F10')
  await expect(headerMenu(page)).toHaveCount(10)
  await expect(headerMenu(page).filter({ hasText: '曲序' })).toHaveCount(0)
  await page.keyboard.press('Home')
  await page.keyboard.press('ArrowDown') // 音轨号在最前，封面是第二项
  await page.keyboard.press('Enter')
  await expect(page.locator('.track-list thead th')).toHaveCount(7)
  await openColumns(page)
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await expect(headerMenu(page)).toHaveCount(0)
})

test('header menu keeps one visible column without changing playlist order', async ({ page }) => {
  await page.getByRole('button', { name: /测试歌单/ }).click()
  await expect(page.locator('[data-pc-section="reorderablerowhandle"]')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__lastReorder)).toBeUndefined()
  for (const label of ['封面', '艺术家', '专辑', '时长', '年份', '载体', '国家']) {
    await openColumns(page)
    await headerMenu(page).filter({ hasText: label }).click()
  }
  await expect(page.locator('.track-list thead th')).toHaveCount(1)
  await openColumns(page)
  await expect(headerMenu(page).filter({ hasText: '标题' })).toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Escape')
  await expect(headerMenu(page)).toHaveCount(0)
})

test('drawer follows local-search and album owned rows but never ghost versions', async ({ page }) => {
  await cell(page, 0).dblclick()
  await expect(drawer(page)).toBeVisible()
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('searchbox').fill('Tetra')
  await page.getByRole('searchbox').press('Enter')
  await expect(rows(page)).toHaveCount(1)
  await cell(page, 0).click()
  await expect(drawer(page).getByRole('heading', { name: 'Tetra.', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await cell(page, 2).click()
  await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  await cell(page, 3).click()
  await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  await expect(rows(page).nth(3)).toHaveAttribute('data-ghost', 'true')
  await expect(page.getByRole('button', { name: '其他版本', exact: true })).toHaveCount(0)
  await cell(page, 3).click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: '加入曲库', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.ListArtists = async () => [{ id: 30, name: '测试艺术家', trackCount: 7 }]
    api.GetArtistHeader = async () => ({ title: '测试艺术家', facets: [] })
  })
  await page.getByRole('button', { name: '艺术家', exact: true }).click()
  await page.getByRole('button', { name: /测试艺术家/ }).click()
  await cell(page, 2).click()
  await expect(drawer(page).getByRole('heading', { name: 'Tetra.', exact: true })).toBeVisible()
})

test('returning from cover search cancels the session and ignores its late result', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, original = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const result = await original(id)
      return { ...result, track: { ...result.track, releaseId: id + 10 } }
    }
    api.StartCoverSearch = async (request: any) => {
      ;(window as any).__lateCover = () => (window as any).runtime.EventsEmit('cover:results', {
        generation: request.generation, searchId: request.searchId, releaseId: request.releaseId, source: request.sources[0], state: 'empty', cards: [], error: '',
      })
    }
    api.CancelCoverSearch = async (id: string) => { (window as any).__cancelledCover = id }
  })
  await cell(page, 0).dblclick()
  await drawer(page).getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  const search = page.getByRole('region', { name: '从网络搜索封面' })
  await expect(search).toBeVisible()
  await search.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(search.getByText('正在查询封面…')).toBeVisible()
  await page.getByRole('button', { name: '关闭封面搜索', exact: true }).click()
  await expect(search).toHaveCount(0)
  await cell(page, 1).click()
  await expect(drawer(page).getByRole('heading', { name: '夕立', exact: true })).toBeVisible()
  await page.evaluate(() => (window as any).__lateCover())
  await expect(search).toHaveCount(0)
  expect(await page.evaluate(() => !!(window as any).__cancelledCover)).toBe(true)
  await drawer(page).getByTitle('关闭', { exact: true }).click()
  await page.evaluate(() => (window as any).__lateCover())
  await expect(drawer(page)).toHaveCount(0)
})

test('enrichment control is compact and still toggles', async ({ page }) => {
  const start = page.getByRole('button', { name: '开始', exact: true })
  const box = (await start.boundingBox())!
  expect(box.height).toBeLessThanOrEqual(24)
  expect(box.width).toBeLessThanOrEqual(64)
  await start.click()
  await page.getByRole('button', { name: '暂停', exact: true }).click()
  await expect(start).toBeVisible()
})
