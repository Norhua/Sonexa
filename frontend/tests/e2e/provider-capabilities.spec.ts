import { test, expect } from './fixtures'
import { readFileSync } from 'node:fs'
const stub = readFileSync(new URL('./wails-stub.js', import.meta.url), 'utf8')

test('QQ settings preserve 30, share the API budget and validate bounded saves', async ({ page }) => {
  await page.addInitScript({ content: stub + `
    const save = window.go.app.App.SaveSettings;
    window.__qqSaves = [];
    window.go.app.App.SaveSettings = async next => {
      window.__qqSaves.push(next.qqmusic.ratePerMin);
      return save(next);
    };
  ` })
  await page.goto('/')
  await page.getByRole('button', { name: '设置', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '设置', exact: true })
  await dialog.getByRole('button', { name: '上游', exact: true }).click()
  const rate = dialog.getByRole('spinbutton', { name: 'QQ 音乐合计速率（请求/分钟）' })
  await expect(rate).toHaveValue('30')
  await expect(rate).toHaveAttribute('min', '1')
  await expect(rate).toHaveAttribute('max', '60')
  await expect(dialog).toContainText('默认 45 请求/分钟')
  await expect(dialog).toContainText('元数据、封面查询与歌词请求合计共用此配额，不含图片下载')
  for (const value of ['', '0', '-1', '0.5', '60.1', '90']) {
    await rate.fill(value)
    await rate.press('Tab')
    await expect(rate).toHaveValue(value)
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect(dialog).toContainText('QQ 音乐合计速率必须是 1–60')
    expect(await page.evaluate(() => (window as any).__qqSaves)).toEqual([])
  }
  for (const value of ['1', '20.5', '60', '30']) {
    await rate.fill(value)
    await rate.press('Tab')
    await dialog.getByRole('button', { name: '保存', exact: true }).click()
    await expect.poll(() => page.evaluate(() => (window as any).__qqSaves.at(-1))).toBe(Number(value))
  }
  await dialog.getByRole('button', { name: '恢复默认', exact: true }).click()
  await expect(rate).toHaveValue('45')
  await dialog.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__qqSaves.at(-1))).toBe(45)
})

test('registry order selects default and capabilities drive country and media controls', async ({ page }) => {
  await page.addInitScript({ content: stub + `
    window.go.app.App.ListProviders = async () => [
      { id: 'qq', displayName: 'QQ 音乐', configured: true, needsCountry: false, capabilities: [] },
      { id: 'itunes', displayName: 'iTunes', configured: true, needsCountry: true, capabilities: [] },
      { id: 'musicbrainz', displayName: 'MusicBrainz', configured: true, needsCountry: false, capabilities: ['MediumFormat', 'Country'] },
    ];
  ` })
  await page.goto('/')
  await page.getByRole('button', { name: /复核队列/ }).click()
  await expect(page.getByRole('combobox')).toHaveText('QQ 音乐')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__lastResearch.providerID)).toBe('qq')
  await expect(page.getByRole('columnheader', { name: '载体', exact: true })).toHaveCount(0)
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'iTunes', exact: true }).click()
  await expect(page.getByRole('combobox')).toHaveCount(2)
  await expect(page.getByRole('columnheader', { name: '载体', exact: true })).toHaveCount(0)
  await page.getByRole('combobox').first().click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await expect(page.getByRole('combobox')).toHaveCount(1)
  await expect(page.getByRole('columnheader', { name: '载体', exact: true })).toBeVisible()
  await expect(page.getByRole('columnheader', { name: '国家/地区', exact: true })).toBeVisible()
})
