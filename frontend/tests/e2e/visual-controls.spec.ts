import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
})
test('settings render all groups, numeric input fits, Select Escape stays in dialog, theme saves', async ({ page }, info) => {
  await page.getByRole('button', { name: '设置', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '设置', exact: true })
  await expect(dialog).toBeVisible()
  await dialog.getByRole('combobox').first().click()
  await page.getByRole('option', { name: '亮色', exact: true }).click()
  await expect(page.locator('html')).not.toHaveClass(/dark/)
  await dialog.getByRole('combobox').first().click()
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(page.getByRole('option')).toHaveCount(0)
  for (const name of ['上游', '匹配', '导入导出', '缓存', '关于', '常规']) {
    await dialog.getByRole('button', { name, exact: true }).click()
    await expect(dialog.getByRole('heading', { name, exact: true })).toBeVisible()
  }
  await dialog.getByRole('button', { name: '缓存', exact: true }).click()
  const numeric = dialog.locator('[data-pc-name="inputnumber"]').first()
  const outer = (await numeric.boundingBox())!, inner = (await numeric.getByRole('spinbutton').boundingBox())!
  expect(inner.width).toBeLessThanOrEqual(outer.width + 2)
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect(dialog).toContainText('已保存')
  const root = (await dialog.boundingBox())!
  const save = dialog.getByRole('button', { name: '保存', exact: true })
  const saveBox = (await save.boundingBox())!
  expect(saveBox.y + saveBox.height).toBeLessThan(root.y + root.height)
  await page.screenshot({ path: info.outputPath('settings-light.png'), animations: 'disabled' })
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
})
test('table keeps styles under PT overrides; sticky header, no horizontal viewport leak, screenshot both themes', async ({ page }, info) => {
  const table = page.getByRole('table', { name: '曲目列表' })
  expect(await table.locator('td').first().evaluate(el => getComputedStyle(el).paddingLeft)).toBe('8px')
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const original = api.ListTracks
    api.ListTracks = async (filter: unknown) => {
      const { tracks } = await original(filter)
      const many = Array.from({ length: 200 }, (_, i) => ({ ...tracks[i % tracks.length], id: i + 1 }))
      return { tracks: many, total: many.length }
    }
  })
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(200)
  const top = (await table.locator('thead').boundingBox())!.y
  const container = page.locator('.track-list [data-pc-section="tablecontainer"]')
  await container.evaluate(el => { el.scrollTop = 1000 })
  await expect.poll(() => container.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
  expect(Math.abs((await table.locator('thead').boundingBox())!.y - top)).toBeLessThan(2)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  for (const dark of [false, true]) {
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    await page.screenshot({ path: info.outputPath(`tracks-${dark ? 'dark' : 'light'}.png`), animations: 'disabled' })
  }
})
test('already-owned candidate jumps to library without admitting again', async ({ page }) => {
  await page.evaluate(() => {
    ;(window as any).go.app.App.SearchUpstream = async () => ({ byProvider: [{
      capabilities: [], rows: [{ provider: 'musicbrainz', matchRef: 'test', badges: ['本地', 'MB'],
        title: '已有曲目', action: '已在库中', localTrackId: 1, confidence: 90 }],
    }] })
    ;(window as any).go.app.App.AddToLibrary = async () => { throw new Error('must not admit twice') }
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('button', { name: '查看库中曲目：已有曲目' }).click()
  await expect(page.locator('aside')).toBeVisible()
})
