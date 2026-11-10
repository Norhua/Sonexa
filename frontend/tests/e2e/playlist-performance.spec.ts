import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
})

test('candidate columns resize and expose complete long album text', async ({ page }) => {
  await page.addInitScript(() => {
    const api = (window as any).go.app.App, original = api.GetReviewQueue
    api.GetReviewQueue = async () => {
      const queue = await original()
      queue.items[0].candidates = [{ provider: 'qq', matchRef: 'candidate', title: '歌曲', artist: '署名', badges: ['qq'], action: '接受', confidence: 90, album: '很长的完整专辑名称 '.repeat(20) }]
      return queue
    }
  })
  await page.goto('/')
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const table = page.getByRole('table', { name: '候选列表' })
  const header = table.getByRole('columnheader', { name: '歌集名', exact: true })
  const before = await header.boundingBox()
  const handle = header.locator('[data-pc-section="columnresizer"]')
  await expect(handle).toBeVisible()
  const box = await handle.boundingBox()
  await page.mouse.move(box!.x + box!.width / 2, box!.y + box!.height / 2)
  await page.mouse.down()
  await page.mouse.move(box!.x + 180, box!.y + box!.height / 2, { steps: 8 })
  await page.mouse.up()
  await expect.poll(async () => (await header.boundingBox())!.width).toBeGreaterThan(before!.width + 140)
  await expect(table.locator('[title]').filter({ hasText: '很长的完整专辑名称' }).first()).toHaveAttribute('title', '很长的完整专辑名称 '.repeat(20))
})

test('batch history retains 30.0 percent after manual acceptance and toggles ratio', async ({ page }) => {
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    w.__accepted = false
    api.GetImportQueue = async () => [{
      id: 1, status: 'running', running: true, sourceKind: 'synced_playlist',
      destinationKind: 'synced_playlist', destinationName: '千首同步歌单',
      processed: 10, totalRows: 15, fileRows: 15, createdAt: '2026-11-10T08:00:00Z', failedTitles: [],
      stats: { imported: w.__accepted ? 8 : 7, review: w.__accepted ? 2 : 3, failed: 0, queued: 5, reviewEntered: 3, processedOnce: 10 },
    }]
  })
  await page.goto('/')
  await page.getByRole('button', { name: '导入队列', exact: true }).click()
  const history = page.getByTestId('batch-history')
  expect(await history.getByRole('columnheader').allTextContents()).toEqual(['批次', '状态', '已导入', '复核中', '失败', '复核率', '目标', '时间'])
  const row = history.locator('tbody tr'), cells = row.getByRole('cell')
  await expect(cells.nth(2)).toHaveText('7')
  await expect(cells.nth(3)).toHaveText('3')
  const ratio = history.getByRole('button', { name: '切换批次 1 复核率显示' })
  await expect(ratio).toHaveText('30.0%')
  await page.evaluate(() => { const w = window as any; w.__accepted = true; w.runtime.EventsEmit('import:queue-changed') })
  await expect(cells.nth(2)).toHaveText('8')
  await expect(cells.nth(3)).toHaveText('2')
  await expect(ratio).toHaveText('30.0%')
  await ratio.click(); await expect(ratio).toHaveText('3 / 10')
  await ratio.press('Enter'); await expect(ratio).toHaveText('30.0%')
  await expect(cells.nth(6)).toHaveText('千首同步歌单')
})

test('large synced playlist renders a bounded window without truncating selection or keyboard navigation', async ({ page }) => {
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    api.ListSyncedPlaylists = async () => [{ id: 77, name: '千首歌单', importedCount: 1200, revision: 1 }]
    api.GetSyncedPlaylistEntries = async () => Array.from({ length: 1200 }, (_, i) => ({
      entryId: i + 1, trackId: i === 1199 ? 1 : i + 1, upstreamPresent: i < 1199, inLibrary: false,
      track: { id: i === 1199 ? 1 : i + 1, title: `歌曲 ${i + 1}`, artist: '署名', album: '专辑', addedAt: '' },
    }))
    api.PrepareLibraryAddition = async (ids: number[]) => ({ id: 'large-add', hash: 'hash', willAdd: ids, alreadyInLibrary: [] })
  })
  await page.goto('/')
  await page.locator('nav').getByRole('button', { name: /千首歌单/ }).click()
  await expect(page.getByRole('status')).toContainText('1200 个已导入项')
  const rows = page.locator('.track-list [data-row-key]')
  await expect.poll(() => rows.count()).toBeGreaterThan(0)
  expect(await rows.count()).toBeLessThan(80)
  const first = page.locator('[data-row-key="synced:1"]')
  await first.click(); await first.press('End')
  const last = page.locator('[data-row-key="synced:1200"]')
  await expect(last).toBeVisible()
  await expect(last).toContainText('歌曲 1200')
  await expect(last).toHaveAttribute('data-upstream-missing', 'true')
  await expect(last).toBeFocused()
  const headerCells = page.locator('.track-list thead th')
  const bodyCells = last.locator('td')
  for (let i = 0; i < await headerCells.count(); i++) {
    const head = await headerCells.nth(i).boundingBox(), body = await bodyCells.nth(i).boundingBox()
    expect(Math.abs(head!.x - body!.x)).toBeLessThan(2)
    expect(Math.abs(head!.width - body!.width)).toBeLessThan(2)
  }
  await last.press('Home')
  await expect(first).toBeVisible(); await expect(first).toBeFocused()
  await first.press('End'); await expect(last).toBeFocused()
  await last.press('ControlOrMeta+a')
  await expect(page.getByRole('status')).toContainText('已选 1199 项')
  await last.click({ button: 'right' })
  await page.getByRole('menuitem', { name: '添加到我的音乐库', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '添加到我的音乐库' })).toContainText('将添加（1199）')
})
