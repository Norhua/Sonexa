import { test, expect } from './fixtures'
import type { Locator, Page } from '@playwright/test'
import { fileURLToPath } from 'node:url'

const albumOf = (row: Locator) => row.getByRole('cell').nth(4).locator('span[title]').first()

async function expectColor(album: Locator, color: 'green' | 'orange' | null, dark = false) {
  if (!color) {
    await expect(album).not.toHaveClass(/text-(green|orange)-/)
    return
  }
  await expect(album).toHaveClass(new RegExp(`text-${color}-700`))
  await expect.poll(() => album.evaluate((el, { color, dark }) => {
    const probe = document.createElement('span')
    probe.style.color = `var(--color-${color}-${dark ? '400' : '700'})`
    document.body.append(probe)
    const expected = getComputedStyle(probe).color
    probe.remove()
    return getComputedStyle(el).color === expected
  }, { color, dark })).toBe(true)
}

async function openSearch(page: Page) {
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
}

async function openEditor(page: Page) {
  await page.locator('.track-list [data-row-key]').first().click({ button: 'right' })
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
  await editor.getByRole('button', { name: '搜索候选', exact: true }).click()
  return editor
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    const get = api.GetReviewQueue, listProviders = api.ListProviders
    api.ListProviders = async () => (await listProviders()).map((provider: any) => ({ ...provider, capabilities: ['PrimaryType'] }))
    const row = { provider: 'musicbrainz', title: '候选歌曲', artist: '歌手', album: '很长的完整专辑名（特别版）', primaryType: 'Album', badges: ['musicbrainz'], action: '接受', confidence: 90, localTrackId: 0 }
    w.__releaseRows = [
      { ...row, matchRef: 'both', releaseExists: true, releaseGroupExists: true },
      { ...row, matchRef: 'group-only', releaseExists: false, releaseGroupExists: true },
      { ...row, matchRef: 'neither', releaseExists: false, releaseGroupExists: false },
      { ...row, matchRef: 'release-only', releaseExists: true, releaseGroupExists: false, reviewOnly: true },
    ]
    api.GetReviewQueue = async () => {
      const queue = await get()
      queue.items[0].candidates = structuredClone(w.__releaseRows)
      return queue
    }
    api.ResearchReviewItem = async () => {
      w.__releaseRows = [{ ...row, matchRef: 'manual', releaseExists: true, releaseGroupExists: true }]
      return structuredClone(w.__releaseRows)
    }
    api.SearchUpstream = async () => ({ byProvider: [{
      capabilities: ['PrimaryType'], rows: structuredClone(w.__releaseRows).map((row: any) => ({ ...row, action: '加入曲库' })),
    }] })
    api.SearchEditCandidates = async () => structuredClone(w.__releaseRows)
    api.RefreshSearchCandidates = async (rows: any[]) => rows.map(row => ({ ...row, ...structuredClone(w.__releaseRows.find((r: any) => r.matchRef === row.matchRef)), action: row.action }))
  })
})

for (const context of ['review', 'search'] as const) {
  test(`${context} colors album names, not sources, and refreshes without changing actions`, async ({ page }, testInfo) => {
    await page.goto('/')
    if (context === 'review') await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
    else await openSearch(page)
    const table = page.getByRole('table', { name: '候选列表' })
    const rows = table.locator('tbody tr')
    await expect(rows).toHaveCount(4)
    const action = context === 'review' ? '接受' : '加入曲库'
    for (const dark of [false, true]) {
      await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
      for (const [i, color] of [[0, 'green'], [1, 'orange'], [2, null], [3, 'green']] as const) {
        const row = rows.nth(i), source = row.getByRole('cell').first()
        await expect(source.locator('[data-pc-name="tag"]')).toHaveCount(1)
        await expect(source).not.toContainText('已存在')
        await expect(source.getByText('本地', { exact: true })).toHaveCount(0)
        await expect(row.getByRole('button', { name: action, exact: true })).toBeEnabled()
        await expectColor(albumOf(row), color, dark)
      }
      await expect(albumOf(rows.first())).toHaveAttribute('title', /很长的完整专辑名（特别版）.*\(Album\).*release 已存在.*包含同步歌单.*不表示该曲目已存在、已入库或可自动导入/)
      await expect(albumOf(rows.nth(1))).toHaveAttribute('title', /同来源的原生 release group/)
      await expect(albumOf(rows.first()).locator('.sr-only')).toContainText('release 已存在')
      await page.setViewportSize({ width: 960, height: 900 })
      await albumOf(rows.first()).scrollIntoViewIfNeeded()
      await page.screenshot({ path: testInfo.outputPath(`${context}-release-state-${dark ? 'dark' : 'light'}.png`) })
    }
    if (context === 'review') await page.getByLabel('曲名', { exact: true }).fill('保留搜索草稿')
    else await page.getByRole('searchbox').fill('保留搜索草稿')
    await page.evaluate(() => {
      const w = window as any
      w.__releaseRows[0].releaseExists = false
      w.runtime.EventsEmit('library:changed')
    })
    await expectColor(albumOf(rows.first()), 'orange', true)
    await page.evaluate(() => {
      const w = window as any
      w.__releaseRows[0].releaseGroupExists = false
      w.runtime.EventsEmit('library:changed')
    })
    await expectColor(albumOf(rows.first()), null)
    await expect(albumOf(rows.first())).not.toHaveAttribute('title', /已存在/)
    await expect(rows.first().getByRole('button', { name: action, exact: true })).toBeEnabled()
    if (context === 'review') {
      await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('保留搜索草稿')
      await page.getByRole('button', { name: '重新搜索', exact: true }).click()
      await expect(rows).toHaveCount(1)
      await expectColor(albumOf(rows.first()), 'green', true)
    } else await expect(page.getByRole('searchbox')).toHaveValue('保留搜索草稿')
    await expect(page.getByRole('dialog')).toHaveCount(0)
  })
}

test('album color coexists with exact local badge and skip, without source release tags', async ({ page }) => {
  await page.addInitScript(() => {
    const w = window as any
    w.__releaseRows = [{ ...w.__releaseRows[0], badges: ['本地', 'musicbrainz'], localTrackId: 7, inLibrary: false, action: '跳过' }]
  })
  await page.goto('/')
  await page.evaluate(() => document.documentElement.classList.remove('dark'))
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const row = page.getByRole('table', { name: '候选列表' }).locator('tbody tr').first()
  const source = row.getByRole('cell').first()
  await expect(source.locator('[data-pc-name="tag"]')).toHaveCount(2)
  await expect(source.locator('[data-pc-name="tag"]').filter({ hasText: /^本地$/ })).toHaveAttribute('title', /不代表已加入音乐库/)
  await expectColor(albumOf(row), 'green')
  await expect(row.getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
})

test('editor candidates share colors and local refresh preserves drafts and the selected preview', async ({ page }, testInfo) => {
  await page.goto('/')
  const editor = await openEditor(page)
  const rows = editor.locator('button.candidate-row')
  await expect(rows).toHaveCount(4)
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    for (const [i, color] of [[0, 'green'], [1, 'orange'], [2, null], [3, 'green']] as const) {
      await expectColor(rows.nth(i).locator('span[title]'), color, dark)
    }
    await page.screenshot({ path: testInfo.outputPath(`editor-release-state-${dark ? 'dark' : 'light'}.png`) })
  }
  await rows.first().click()
  await expect(rows.first()).toHaveAttribute('aria-pressed', 'true')
  await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('保留手工输入')
  await editor.getByRole('textbox', { name: '搜索曲名', exact: true }).fill('保留搜索输入')
  await page.evaluate(() => {
    const w = window as any
    Object.assign(w.__releaseRows[0], { releaseExists: false, releaseGroupExists: true, localTrackId: 7, inLibrary: false })
    w.runtime.EventsEmit('library:changed')
  })
  await expectColor(rows.first().locator('span[title]'), 'orange', true)
  await expect(rows.first()).toContainText('本地已存在，未入库')
  await expect(rows.first()).toHaveAttribute('aria-pressed', 'true')
  await expect(editor.getByRole('region', { name: '上游歌曲信息' })).toContainText('候选歌曲')
  await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('保留手工输入')
  await expect(editor.getByRole('textbox', { name: '搜索曲名', exact: true })).toHaveValue('保留搜索输入')
})

test('editor ignores a late local refresh after a new search or closing', async ({ page }) => {
  await page.goto('/')
  const editor = await openEditor(page)
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.RefreshSearchCandidates = (rows: any[]) => new Promise(resolve => { w.__finishLocal = () => resolve(rows) })
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLocal)).toBe('function')
  await page.evaluate(() => { const w = window as any; w.__releaseRows = [{ ...w.__releaseRows[2], matchRef: 'new', album: '新搜索专辑' }] })
  await editor.getByRole('button', { name: '搜索候选', exact: true }).click()
  await page.evaluate(() => (window as any).__finishLocal())
  const rows = editor.locator('button.candidate-row')
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText('新搜索专辑')
  await expectColor(rows.first().locator('span[title]'), null)
  await page.evaluate(() => {
    const w = window as any
    delete w.__finishLocal
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLocal)).toBe('function')
  await editor.getByRole('button', { name: '取消', exact: true }).click()
  await page.evaluate(() => (window as any).__finishLocal())
  await expect(editor).toHaveCount(0)
})

test('editor refreshes changes during search and rejects old-source local results', async ({ page }) => {
  await page.goto('/')
  const editor = await openEditor(page)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    api.SearchEditCandidates = () => {
      const rows = structuredClone(w.__releaseRows)
      return new Promise(resolve => { w.__finishSearch = () => resolve(rows) })
    }
  })
  await editor.getByRole('button', { name: '搜索候选', exact: true }).click()
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishSearch)).toBe('function')
  await page.evaluate(() => {
    const w = window as any
    Object.assign(w.__releaseRows[0], { releaseExists: false, releaseGroupExists: false })
    w.runtime.EventsEmit('library:changed')
    w.__finishSearch()
  })
  const rows = editor.locator('button.candidate-row')
  await expectColor(rows.first().locator('span[title]'), null)
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.RefreshSearchCandidates = (rows: any[]) => new Promise(resolve => { w.__finishLocal = () => resolve(rows) })
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLocal)).toBe('function')
  await editor.getByRole('combobox', { name: '上游来源' }).click()
  await page.getByRole('option', { name: 'QQ 音乐', exact: true }).click()
  await page.evaluate(() => (window as any).__finishLocal())
  await expect(rows).toHaveCount(0)
  await expect(editor.getByRole('textbox', { name: '搜索曲名', exact: true })).toHaveValue('夕立')
})
