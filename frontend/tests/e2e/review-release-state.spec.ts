import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    const get = api.GetReviewQueue
    const row = { provider: 'musicbrainz', title: '候选歌曲', artist: '歌手', album: '专辑', badges: ['musicbrainz'], action: '接受', confidence: 90, localTrackId: 0 }
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
  })
})

test('review source column shows independent release identities, not track ownership, and refreshes', async ({ page }, testInfo) => {
  await page.goto('/')
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const table = page.getByRole('table', { name: '候选列表' })
  const rows = table.locator('tbody tr')
  await expect(rows).toHaveCount(4)
  for (const [i, release, group] of [[0, true, true], [1, false, true], [2, false, false], [3, true, false]] as const) {
    const source = rows.nth(i).getByRole('cell').first()
    await expect(source.getByText('release 已存在', { exact: true })).toHaveCount(release ? 1 : 0)
    await expect(source.getByText('release group 已存在', { exact: true })).toHaveCount(group ? 1 : 0)
    await expect(source.getByText('本地', { exact: true })).toHaveCount(0)
    await expect(rows.nth(i).getByRole('button', { name: '接受', exact: true })).toBeEnabled()
  }
  const releaseTag = rows.first().locator('[data-pc-name="tag"]').filter({ hasText: /^release 已存在$/ })
  await expect(releaseTag).toHaveAttribute('title', /包含同步歌单.*不表示该曲目已存在或可自动导入/)
  await expect(rows.first().locator('[data-pc-name="tag"]').filter({ hasText: /^release group 已存在$/ })).toHaveAttribute('title', /同来源的原生/)
  // Both themes and a narrow window still expose the complete badge labels.
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await page.setViewportSize({ width: 960, height: 900 })
    await expect(releaseTag).toBeVisible()
    await expect(rows.first().getByText('release group 已存在', { exact: true })).toBeVisible()
    await page.screenshot({ path: testInfo.outputPath(`review-release-state-${dark ? 'dark' : 'light'}.png`) })
  }
  await page.getByLabel('曲名', { exact: true }).fill('保留搜索草稿')
  await page.evaluate(() => {
    const w = window as any
    w.__releaseRows[0].releaseExists = false
    w.__releaseRows[0].releaseGroupExists = false
    w.runtime.EventsEmit('library:changed')
  })
  await expect(rows.first().getByText('release 已存在', { exact: true })).toHaveCount(0)
  await expect(rows.first().getByText('release group 已存在', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('保留搜索草稿')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect(rows).toHaveCount(1)
  await expect(rows.first().getByText('release 已存在', { exact: true })).toBeVisible()
  await expect(rows.first().getByRole('button', { name: '接受', exact: true })).toBeEnabled()
  await expect(page.getByRole('dialog')).toHaveCount(0)
})

test('release badges coexist with exact local track badge and skip action', async ({ page }) => {
  await page.addInitScript(() => {
    const w = window as any
    w.__releaseRows = [{ ...w.__releaseRows[0], badges: ['本地', 'musicbrainz'], localTrackId: 7, inLibrary: false, action: '跳过' }]
  })
  await page.goto('/')
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const row = page.getByRole('table', { name: '候选列表' }).locator('tbody tr').first()
  const source = row.getByRole('cell').first()
  await expect(source.locator('[data-pc-name="tag"]')).toHaveCount(4)
  await expect(source.locator('[data-pc-name="tag"]').filter({ hasText: /^本地$/ })).toHaveAttribute('title', /不代表已加入音乐库/)
  await expect(row.getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
})

test('ordinary search does not display review-only release state badges', async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.SearchUpstream = async () => ({ byProvider: [{
      capabilities: [], rows: [{ ...w.__releaseRows[0], action: '加入曲库' }],
    }] })
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  const table = page.getByRole('table', { name: '候选列表' })
  await expect(table.getByRole('button', { name: '加入曲库', exact: true })).toBeVisible()
  await expect(table.getByText('release 已存在', { exact: true })).toHaveCount(0)
  await expect(table.getByText('release group 已存在', { exact: true })).toHaveCount(0)
})
