import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    const row = (id: number, problemId: number, batchId: number, title: string, album: string) => ({
      id, problemId, batchId, revision: 2, rowIndex: id === 11 ? 2 : 7, status: id === 12 ? 'failed' : 'review',
      batchTag: batchId === 1 ? '/imports/original.csv' : '/imports/aliases.tsv',
      inTitle: title, inArtist: 'Artist', inAlbum: album, inYear: 2020, inDiscNo: 2, inTrackNo: 3, inDurationMs: 620000,
      profile: { duration: { raw: '10:20', state: 'known', value: 620000 }, year: { raw: '未知年份', state: 'invalid' } },
      rawRow: JSON.stringify([title, 'Artist', album, '10:20', '保留未映射列']),
      candidates: [], triedVariants: [], attemptCount: id === 12 ? 5 : 0,
      lastError: id === 12 ? '来源暂不可用' : null, lastErrorAt: id === 12 ? '2026-10-01T01:00:00Z' : null,
      reviewReason: 'ambiguous', reviewEvidence: JSON.stringify({ reasons: [{ code: 'position_ambiguous', field: 'track', detail: '同发行存在多个位置，不能自动确定' }] }),
    })
    // Membership is the backend fixture result, never browser-side text dedup.
    w.__problemRows = [row(11, 101, 1, 'Song', 'Album'), row(12, 101, 2, 'Song（译名：歌曲）', 'Album'), row(13, 202, 2, 'Song', 'Other Album')]
    api.GetReviewQueue = async () => ({ items: JSON.parse(JSON.stringify(w.__problemRows)), total: w.__problemRows.length, pending: 2, failed: 1 })
  })
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
})

test('two alias sources appear as one problem with complete originals, failures and operation scope', async ({ page }) => {
  await expect(page.locator('main header')).toContainText('2 个待处理问题 · 待选 2 · 失败 0 · 涉及 3 行')
  await expect(page.getByTestId('problem-title')).toHaveText('Song')
  await expect(page.getByText('1 / 2', { exact: true })).toBeVisible()
  await expect(page.getByTestId('problem-scope')).toContainText('上述 2 行')
  await expect(page.getByText('立即重试仅处理输入 #12。', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '展开来源（2 行）' }).click()
  const first = page.locator('[data-source-id="11"]'), second = page.locator('[data-source-id="12"]')
  await expect(first).toContainText('/imports/original.csv · 批次 #1 · 第 3 行')
  await expect(second).toContainText('/imports/aliases.tsv · 批次 #2 · 第 8 行')
  await expect(first).toContainText('未知年份（无法解析） / 2 / 3 / 10:20')
  await expect(second).toContainText('Song（译名：歌曲）')
  await expect(second).toContainText('已重试 5 次 · 最后错误：来源暂不可用')
  await expect(first.getByTestId('review-reasons')).toContainText('同发行存在多个位置，不能自动确定')
  await first.locator('summary').click()
  await expect(first.locator('pre')).toHaveText('["Song","Artist","Album","10:20","保留未映射列"]')
  await second.locator('summary').click()
  await expect(second.locator('pre')).toContainText('Song（译名：歌曲）')
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByText('2 / 2', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '展开来源（1 行）' }).click()
  await expect(page.locator('[data-source-id="13"]')).toContainText('Other Album')
  await expect(page.getByTestId('problem-scope')).toContainText('上述 1 行')
  expect(await page.evaluate(() => (window as any).__calls.filter((name: string) => name === 'ResearchReviewItem'))).toEqual([])
})

test('same-count source changes refresh by stable problem ID and preserve local draft until explicit reopen', async ({ page }) => {
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByText('2 / 2', { exact: true })).toBeVisible()
  await page.getByLabel('曲名', { exact: true }).fill('尚未提交草稿')
  await page.evaluate(() => {
    const w = window as any
    w.__problemRows[2].revision++
    w.__problemRows[2].batchTag = '/imports/updated.tsv'
    w.__reviewRevision = 2
    w.runtime.EventsEmit('review:changed', { revision: 2 })
  })
  await expect(page.getByText(/问题范围或证据已变化/)).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('尚未提交草稿')
  await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '刷新队列', exact: true }).click()
  await expect(page.getByText(/问题范围或证据已变化/)).toHaveCount(0)
  await page.getByRole('button', { name: '展开来源（1 行）' }).click()
  await expect(page.locator('[data-source-id="13"]')).toContainText('/imports/updated.tsv')
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('尚未提交草稿')
  await expect(page.getByText('2 / 2', { exact: true })).toBeVisible()
  await expect(page.locator('main header')).toContainText('2 个待处理问题')
})
