import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test('enrichment belongs only to all tracks and disappears when pending work finishes', async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  const progress = page.getByTestId('enrichment-progress')
  await expect(progress).toContainText('0/3')
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await expect(progress).toHaveCount(0)
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /测试专辑/ }).click()
  await expect(progress).toHaveCount(0)
  await page.getByRole('button', { name: '全部曲目', exact: true }).click()
  await page.evaluate(() => (window as any).runtime.EventsEmit('enrich:progress', { state: 'running', done: 1, total: 3, pending: 2 }))
  await expect(progress).toContainText('1/3')
  await page.evaluate(() => (window as any).runtime.EventsEmit('enrich:progress', { state: 'idle', done: 3, total: 3, pending: 0 }))
  await expect(progress).toHaveCount(0)
})
