import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => { const d = await detail(id); return { ...d, track: { ...d.track, releaseId: 20 } } }
    let first = true
    api.StartCoverSearch = async (request: any) => {
      const source = request.sources[0]
      const card = (id: number, type: string) => ({ source, sourceRef: 'album', image: { id, title: `测试${type}`, artist: '艺术家', primaryType: type, variants: [{ id: id * 10, spec: '800', constructed: true }] } })
      const event = { generation: request.generation, searchId: request.searchId, releaseId: request.releaseId, source, state: 'ok', cards: source === 'qq' ? [card(1, 'Front'), card(2, 'Back')] : [], error: '' }
      if (source === 'caa' && first) { first = false; event.state = 'error'; event.error = 'CAA 暂时不可用' }
      else if (!event.cards.length) event.state = 'empty'
      ;(window as any).runtime.EventsEmit('cover:results', event)
    }
    api.AdoptCoverCandidate = async (session: string, variant: number, revision: number) => {
      ;(window as any).__coverAdoption = { session, variant, revision }
      ;(window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 1 })
      return { releaseId: 20, revision: 1, width: 800, height: 800, variantId: variant }
    }
  })
  await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="800" height="800"><rect width="800" height="800" fill="#a9598a"/></svg>' }))
})

test('four source page previews labels, preserves other results on retry, and confirms adoption', async ({ page }, testInfo) => {
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  const view = page.getByRole('region', { name: '从网络搜索封面' })
  await expect(view.getByLabel('歌曲', { exact: true })).toHaveValue('綿雪の夢')
  await view.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(view.locator('article')).toHaveCount(2)
  await expect(view.getByText('CAA：CAA 暂时不可用', { exact: true })).toBeVisible()
  const front = view.locator('article').first()
  await expect(front.locator('span').filter({ hasText: /^800×800$/ })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__coverAdoption)).toBeUndefined()
  await view.getByRole('button', { name: '失败·重试', exact: true }).click()
  await expect(view.locator('article')).toHaveCount(2)
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await page.screenshot({ path: testInfo.outputPath(`cover-search-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
  await front.getByRole('button', { name: '选择 QQ 测试Front', exact: true }).click()
  await view.getByRole('button', { name: '应用到专辑', exact: true }).click()
  await expect(view.getByRole('status')).toContainText('800×800')
  expect((await page.evaluate(() => (window as any).__coverAdoption)).variant).toBe(10)
  await view.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.locator('aside').getByRole('heading', { name: '綿雪の夢', exact: true })).toBeVisible()
})

test('search edits sources and country without changing binding and returns to the filtered drawer', async ({ page }) => {
  const query = page.getByPlaceholder('在“全部曲目”中搜索')
  await query.fill('綿雪')
  await query.press('Enter')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(1)
  await page.evaluate(() => {
    const api = (window as any).go.app.App, start = api.StartCoverSearch
    ;(window as any).__searchRequests = []
    api.StartCoverSearch = async (request: any) => { (window as any).__searchRequests.push(request); return start(request) }
    for (const name of ['AddToLibrary', 'ApplyEdits', 'ApplyDetailToTrack', 'ClearProvenance']) {
      api[name] = async () => { throw new Error(`cover search must not call ${name}`) }
    }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  const view = page.getByRole('region', { name: '从网络搜索封面' })
  await expect(view).toContainText('修改后应用到整个专辑')
  await view.getByLabel('歌曲', { exact: true }).fill('新的曲名')
  await view.getByLabel('艺术家', { exact: true }).fill('新的艺术家')
  await view.getByLabel('专辑', { exact: true }).fill('新的查询专辑')
  for (const name of ['CAA', 'QQ', '网易云']) await view.getByRole('checkbox', { name, exact: true }).uncheck()
  await view.getByRole('combobox', { name: '封面来源地区' }).click()
  await page.getByRole('option', { name: '日本', exact: true }).click()
  await view.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(view.getByText('未找到图片', { exact: true })).toBeVisible()
  const requests = await page.evaluate(() => (window as any).__searchRequests)
  expect(requests).toHaveLength(1)
  expect(requests[0]).toMatchObject({ title: '新的曲名', artist: '新的艺术家', album: '新的查询专辑', country: 'JP', sources: ['itunes'], releaseId: 20 })
  expect(await page.evaluate(() => (window as any).__coverAdoption)).toBeUndefined()
  await view.getByRole('button', { name: '取消', exact: true }).click()
  await expect(query).toHaveValue('綿雪')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(1)
  await expect(page.locator('aside h2')).toHaveText('綿雪の夢')
})

test('a registered non-iTunes source controls country input and is the only default request', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.ListCoverSources = async () => [{ id: 'regional-only', name: '地区封面', needsCountry: true }]
    ;(window as any).__searchRequests = []
    api.StartCoverSearch = async (request: any) => { (window as any).__searchRequests.push(request) }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  const view = page.getByRole('region', { name: '从网络搜索封面' })
  const source = view.getByRole('checkbox', { name: '地区封面', exact: true })
  await expect(view.getByRole('checkbox')).toHaveCount(1)
  await expect(source).toBeChecked()
  await expect(view.getByRole('combobox', { name: '封面来源地区' })).toBeVisible()
  await source.uncheck()
  await expect(view.getByRole('combobox', { name: '封面来源地区' })).toHaveCount(0)
  await source.check()
  await view.getByRole('combobox', { name: '封面来源地区' }).click()
  await page.getByRole('option', { name: '日本', exact: true }).click()
  await view.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__searchRequests)).toEqual([
    expect.objectContaining({ sources: ['regional-only'], country: 'JP' }),
  ])
})

test('cover search is a modal overlay, preserves background layout and scroll, and closes one layer at a time', async ({ page }, info) => {
  await page.setViewportSize({ width: 1100, height: 760 })
  await page.evaluate(() => {
    const api = (window as any).go.app.App, list = api.ListTracks
    api.ListTracks = async (...args: any[]) => {
      const result = await list(...args)
      return { tracks: Array.from({ length: 80 }, (_, i) => ({ ...result.tracks[i % 7], id: i + 1 })), total: 80 }
    }
    ;(window as any).runtime.EventsEmit('library:changed')
  })
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(80)
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const trigger = page.getByRole('button', { name: '更新歌曲图片', exact: true })
  const container = page.locator('.track-list [data-pc-section="tablecontainer"]')
  await container.evaluate(el => { el.scrollTop = 500 })
  const originalScroll = await container.evaluate(el => el.scrollTop)
  const mainBounds = await page.locator('main').boundingBox()
  const asideBounds = await page.locator('aside').boundingBox()
  await trigger.click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await page.getByRole('menuitem', { name: '从网络搜索', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索封面', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog).toHaveAttribute('aria-modal', 'true')
  await expect(dialog.locator('aside')).toHaveCount(0)
  expect(await page.locator('main').boundingBox()).toEqual(mainBounds)
  expect(await page.locator('aside').boundingBox()).toEqual(asideBounds)
  expect(await page.locator('main').evaluate(el => !!el.closest('[inert]'))).toBe(true)
  const bounds = (await dialog.boundingBox())!
  expect(bounds.width).toBeLessThan(1100)
  expect(bounds.height).toBeLessThan(760)
  expect(Math.abs(bounds.x + bounds.width / 2 - 550)).toBeLessThan(2)
  await expect(dialog.getByLabel('歌曲', { exact: true })).toBeFocused()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(dialog.locator('article')).toHaveCount(2)
  await dialog.locator('article').first().getByRole('button', { name: '查看大图' }).click()
  await expect(page.getByRole('dialog', { name: '封面预览', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog', { name: '封面预览', exact: true })).toHaveCount(0)
  await expect(dialog).toBeVisible()
  await page.screenshot({ path: info.outputPath('cover-search-modal.png'), animations: 'disabled' })
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
  expect(await container.evaluate(el => el.scrollTop)).toBe(originalScroll)
  expect(await page.locator('main').boundingBox()).toEqual(mainBounds)
})

test('keyboard menu imports from the nil region and restores focus on Escape and cancel', async ({ page }) => {
  await page.route('**/cover?*', route => route.fulfill({ status: 404, body: '' }))
  await page.evaluate(() => {
    ;(window as any).go.app.App.PickAndAdoptCover = async (releaseId: number) => {
      ;(window as any).__pickedRelease = releaseId
      return { cancelled: true }
    }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const trigger = page.getByRole('button', { name: '更新歌曲图片', exact: true })
  await expect(trigger.getByText('nil', { exact: true })).toBeVisible()
  await trigger.click({ button: 'right' })
  await page.keyboard.press('Escape')
  await expect(trigger).toBeFocused()
  await page.keyboard.press('Shift+F10')
  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('ArrowRight')
  await page.keyboard.press('Enter')
  await expect.poll(() => page.evaluate(() => (window as any).__pickedRelease)).toBe(20)
  await expect(page.getByRole('menuitem')).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

test('missing album disables all cover actions without creating a target', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App, detail = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const value = await detail(id)
      return { ...value, track: { ...value.track, releaseId: null } }
    }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await expect(page.getByText('此曲目没有所属专辑，无法更新共享封面')).toBeVisible()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  for (const name of ['从本地存储中选择', '从网络搜索']) {
    await expect(page.getByRole('menuitem', { name, exact: true })).toHaveAttribute('aria-disabled', 'true')
  }
  await expect(page.getByRole('menuitem', { name: '清除封面', exact: true })).toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: '更新歌曲图片', exact: true })).toBeFocused()
})

test('outbound disabled keeps local import available and cancel leaves current cover', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.PickAndAdoptCover = async () => { (window as any).__picked = true; return { cancelled: true } }
  })
  await page.getByRole('button', { name: '设置', exact: true }).click()
  const settings = page.getByRole('dialog', { name: '设置', exact: true })
  await settings.getByRole('button', { name: '上游', exact: true }).click()
  await settings.getByPlaceholder('邮箱或主页地址').fill('')
  await settings.getByRole('button', { name: '保存', exact: true }).click()
  await expect(settings).toContainText('已保存，改动立即生效')
  await page.keyboard.press('Escape')
  await expect(page.getByText('需要设置联系方式', { exact: true })).toBeVisible()
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '更新歌曲图片', exact: true }).hover()
  await expect(page.getByRole('menuitem', { name: '从网络搜索', exact: true })).toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('menuitem', { name: '从本地存储中选择', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__picked)).toBe(true)
  expect(await page.evaluate(() => (window as any).__coverAdoption)).toBeUndefined()
})

test('clear cover asks for confirmation and clears the menu release', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.ClearCover = async (releaseId: number, revision: number) => {
      (window as any).__cleared = [releaseId, revision]
      return { releaseId, revision: revision + 1 }
    }
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '清除封面', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '清除专辑封面？' })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__cleared)).toBeUndefined()
  await page.getByRole('button', { name: '更新歌曲图片', exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '清除封面', exact: true }).click()
  await page.getByRole('dialog', { name: '清除专辑封面？' }).getByRole('button', { name: '清除', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__cleared)).toEqual([20, 0])
})
