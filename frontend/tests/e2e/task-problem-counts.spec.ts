import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test('cross-batch problem counts, existing skips and same-count changes stay separate from source rows', async ({ page }, testInfo) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    const p = { reviewProblems: 1, pendingProblems: 1, failedProblems: 0, sourceRows: 1 }
    w.__taskBatches = [1, 2].map(id => ({ id, filename: `batch-${id}.csv`, status: 'done', running: false, processed: 1, totalRows: 1, fileRows: 1, failedTitles: [], createdAt: '2026-10-01T01:00:00Z',
      stats: { total: 1, queued: 0, matching: 0, imported: 0, review: 1, failed: 0, skipped: 0, discarded: 0, existingSkipped: 0, unmatched: 1, problems: { ...p } } }))
    w.__globalProblems = { ...p, sourceRows: 2 }
    api.GetImportQueue = async () => JSON.parse(JSON.stringify(w.__taskBatches))
    api.GetTaskSummary = async () => ({ activeBatch: 0, filename: '', processed: 0, total: 0, stats: null, running: false, reviewPending: w.__globalProblems.reviewProblems, problems: { ...w.__globalProblems } })
    w.runtime.EventsEmit('tasks:changed', { revision: 2 })
  })
  const badge = page.locator('nav').getByRole('button', { name: /复核队列/ })
  await expect(badge).toHaveText(/复核队列\s*1$/)
  await expect(badge).toHaveAttribute('title', '1 个待处理问题，涉及 2 行；待选 1，失败 0')
  await page.locator('nav').getByRole('button', { name: /导入队列/ }).click()
  await expect(page.getByText('本批涉及 1 个问题，1 行来源；全局 1 个问题，涉及 2 行（跨批不重复计数）。', { exact: true })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '待复核（行）' })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '问题数', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('cross-batch-problem-counts.png') })
  await page.evaluate(() => {
    const w = window as any
    w.__taskBatches[1].filename = 'changed-source.csv'
    w.runtime.EventsEmit('tasks:changed', { revision: 3 })
  })
  await expect(page.locator('[data-batch-id="2"]')).toContainText('changed-source.csv')
  await page.evaluate(() => {
    const w = window as any
    w.__globalProblems = { reviewProblems: 0, pendingProblems: 0, failedProblems: 0, sourceRows: 0 }
    w.__taskBatches.forEach((b: any, i: number) => Object.assign(b.stats, { imported: i === 0 ? 1 : 0, review: 0, unmatched: 0, skipped: i === 1 ? 1 : 0, existingSkipped: i === 1 ? 1 : 0, problems: { ...w.__globalProblems } }))
    w.runtime.EventsEmit('tasks:changed', { revision: 4 })
  })
  await expect(badge).toHaveText('复核队列')
  await expect(page.getByText('全局 0 个问题，涉及 0 行', { exact: false })).toBeVisible()
  const first = page.locator('[data-batch-id="1"] td'), second = page.locator('[data-batch-id="2"] td')
  await expect(first.nth(2)).toHaveText('1'); await expect(first.nth(3)).toHaveText('0')
  await expect(second.nth(2)).toHaveText('0'); await expect(second.nth(3)).toHaveText('1')
  await expect(page.getByRole('button', { name: /到复核队列/ })).toHaveCount(0)
})
