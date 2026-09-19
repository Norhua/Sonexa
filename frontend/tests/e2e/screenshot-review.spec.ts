import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'
const rows = (page: Page) => page.locator('.track-list [data-row-key]')
const column = (page: Page) => page.getByRole('columnheader', { name: '音轨号', exact: true })
async function toggleTrackNumber(page: Page) {
  await page.locator('.track-list thead th').first().click({ button: 'right' })
  await page.getByRole('menuitemcheckbox', { name: '音轨号', exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    const api = (window as any).go.app.App
    api.ListCollections = async () => []
    const listProviders = api.ListProviders
    api.ListProviders = async () => (await listProviders()).map((p: any) => ({
      ...p, capabilities: p.id === 'musicbrainz' ? ['Label', 'CatalogNumber', 'MediumFormat', 'Country', 'PrimaryType'] : [],
    }))
    api.ListArtists = async () => [
      { id: 30, name: 'Kirara Magic', trackCount: 1 },
      { id: 31, name: 'Kitsui Akira', trackCount: 1 },
    ]
    api.GetArtistHeader = async () => ({ facets: [] })
  })
  await page.goto('/')
  await expect(rows(page)).toHaveCount(7)
})

test('empty navigation is indented and middle headings stay small and fixed', async ({ page }, testInfo) => {
  for (const name of ['歌单', '标签']) {
    const section = page.locator('nav section').filter({ has: page.getByRole('heading', { name, exact: true }) })
    const empty = section.getByText(`还没有${name}`, { exact: true })
    const inset = await empty.evaluate(el => Number.parseFloat(getComputedStyle(el).paddingLeft))
    expect(inset).toBe(24)
    const icon = (await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).locator('svg').boundingBox())!
    expect((await empty.boundingBox())!.x + inset).toBe(icon.x)
  }
  await page.getByRole('button', { name: '艺术家', exact: true }).click()
  const heading = page.getByRole('heading', { name: '艺术家', exact: true })
  for (const name of ['Kirara Magic', 'Kitsui Akira']) {
    await page.getByRole('button', { name: new RegExp(name) }).click()
    await expect(heading).toBeVisible()
    expect(await heading.evaluate(el => getComputedStyle(el).fontWeight)).toBe('400')
    expect(await heading.evaluate(el => getComputedStyle(el).fontSize)).toBe('14px')
  }
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(page.getByRole('heading', { name: '专辑', exact: true })).toBeVisible()
  await page.evaluate(() => document.documentElement.classList.remove('dark'))
  await expect.poll(() => page.locator('.track-list thead th').first().evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(255, 255, 255)')
  await page.screenshot({ path: testInfo.outputPath('album-heading-light.png') })
})

test('track number defaults to album only; explicit preferences survive view changes and reload', async ({ page }) => {
  await expect(column(page)).toHaveCount(0)
  await toggleTrackNumber(page)
  await expect(column(page)).toBeVisible()
  await expect(rows(page).first().locator('td').first()).toHaveText('1')
  await expect(page.locator('.track-list thead th').first()).toHaveText('音轨号')
  await expect(page.locator('.track-list thead th').nth(1)).toHaveText('封面')
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(column(page)).toBeVisible()
  await expect(rows(page).nth(1).locator('td').first()).toHaveText('2') // ghost position
  await toggleTrackNumber(page)
  await expect(column(page)).toHaveCount(0)
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect(column(page)).toBeVisible()
  await page.reload()
  await expect(column(page)).toBeVisible()
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(column(page)).toHaveCount(0)
  await expect(page.locator('[data-pc-section="reorderablerowhandle"]')).toHaveCount(0)
})

test('drawer shows one complete vocal group, single-column release data and no field sources', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => ({
      ...await original(id),
      creditText: 'Kirara Magic feat. Kitsui Akira',
      sources: [{ key: 'title', label: '标题', source: 'musicbrainz' }],
      metadata: { year: 2024, trackNumber: 1, primaryType: 'single', mediumFormat: 'Digital Media' },
      participants: [{ role: 'vocal', label: '演唱', artists: [
        { artistId: 30, name: 'Kirara Magic' }, { artistId: 31, name: 'Kitsui Akira' },
      ] }],
    })
  })
  await rows(page).nth(4).locator('td').last().dblclick()
  const drawer = page.locator('aside')
  await expect(drawer).toBeVisible()
  expect((await drawer.boundingBox())!.width).toBe(340)
  await expect(drawer.locator('dt').filter({ hasText: '演唱' })).toHaveCount(1)
  await expect(drawer.getByRole('button', { name: 'Kirara Magic', exact: true })).toBeVisible()
  await expect(drawer.getByRole('button', { name: 'Kitsui Akira', exact: true })).toBeVisible()
  await expect(drawer.getByText('字段来源')).toHaveCount(0)
  await expect(drawer.getByText('标题·musicbrainz')).toHaveCount(0)
  const release = drawer.locator('section').filter({ has: page.getByRole('heading', { name: '发行信息' }) })
  const labels = release.locator('dt')
  const coords = await labels.evaluateAll(els => els.map(el => ({ x: el.getBoundingClientRect().x, y: el.getBoundingClientRect().y })))
  expect(new Set(coords.map(p => p.x)).size).toBe(1)
  expect(new Set(coords.map(p => p.y)).size).toBe(8)
  await page.screenshot({ path: testInfo.outputPath('compact-detail.png') })
  await drawer.getByRole('button', { name: '编辑', exact: true }).click()
  await expect(page.getByRole('dialog').getByText('用户', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('dialog').getByText('歌集', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await drawer.getByRole('button', { name: 'Kitsui Akira', exact: true }).click()
  await expect(page.getByRole('heading', { name: '艺术家', exact: true })).toBeVisible()
})

test('review removes marked text, moves actions below inputs and restores only the search draft', async ({ page }, testInfo) => {
  // 用能力齐全的上游证明列被移除，而不是因为假数据未声明能力才隐藏。
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.GetReviewQueue
    api.GetReviewQueue = async () => {
      const queue = await original()
      queue.items[0].candidates = [{
        title: 'candidate', provider: 'musicbrainz', badges: ['musicbrainz'],
        action: '接受', confidence: 90, trackNumber: 2, trackTotal: 12,
        label: 'should not display', catalogNumber: 'TEST-001',
      }]
      return queue
    }
  })
  await page.getByRole('button', { name: /复核队列/ }).click()
  for (const text of ['导入时的原始文本', '改搜只影响', '已尝试变体', 'should not display', 'TEST-001']) {
    await expect(page.getByText(text, { exact: false })).toHaveCount(0)
  }
  await expect(page.getByRole('heading', { name: '重新搜索', exact: true })).toHaveCount(0)
  await expect(page.getByText('4円（ヨエン） · side-c', { exact: true })).toHaveCount(0)
  await expect(page.getByText('来自 music_extracted.csv')).toBeVisible()
  for (const name of ['第几曲', '厂牌', '目录号', '动作']) await expect(page.getByRole('columnheader', { name, exact: true })).toHaveCount(0)
  await expect(page.getByRole('columnheader', { name: '音轨', exact: true })).toBeVisible()
  await expect(page.getByRole('cell', { name: '2/12', exact: true })).toBeVisible()
  const title = page.getByRole('textbox', { name: '曲名', exact: true })
  const artist = page.getByRole('textbox', { name: '艺术家', exact: true })
  const album = page.getByRole('textbox', { name: '专辑', exact: true })
  await title.fill('changed title'); await artist.fill('changed artist'); await album.fill('changed album')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__lastResearch?.title)).toBe('changed title')
  const restore = page.getByRole('button', { name: '恢复原始信息', exact: true })
  const noMatch = page.getByRole('button', { name: '无需匹配', exact: true })
  expect((await restore.boundingBox())!.y).toBeGreaterThan((await title.boundingBox())!.y)
  expect((await restore.boundingBox())!.x).toBeLessThan((await noMatch.boundingBox())!.x)
  const before = await page.evaluate(() => (window as any).__calls.filter((s: string) => s === 'ResearchReviewItem').length)
  await restore.click()
  await expect(title).toHaveValue('ツバサ（羽翼）')
  await expect(artist).toHaveValue('4円（ヨエン）')
  await expect(album).toHaveValue('side-c')
  expect(await page.evaluate(() => (window as any).__calls.filter((s: string) => s === 'ResearchReviewItem').length)).toBe(before)
  await page.screenshot({ path: testInfo.outputPath('review-simplified.png') })
  await page.getByRole('button', { name: '下一条 →', exact: true }).click()
  await title.fill('changed next')
  await restore.click()
  await expect(title).toHaveValue('you')
  await noMatch.click()
  await expect.poll(() => page.evaluate(() => (window as any).__calls.includes('NoMatchNeeded'))).toBe(true)
})

test('late review research cannot overwrite the next item', async ({ page }) => {
  await page.evaluate(() => {
    ;(window as any).go.app.App.ResearchReviewItem = () => new Promise(resolve => {
      ;(window as any).__resolveResearch = () => resolve([{ title: 'STALE', badges: [], action: '接受', confidence: 1 }])
    })
  })
  await page.getByRole('button', { name: /复核队列/ }).click()
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await page.getByRole('button', { name: '下一条 →', exact: true }).click()
  await expect(page.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('you')
  await page.evaluate(() => (window as any).__resolveResearch())
  await expect(page.getByText('STALE', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeEnabled()
})
