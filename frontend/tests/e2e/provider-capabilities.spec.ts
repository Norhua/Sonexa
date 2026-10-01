import { test, expect } from './fixtures'
import { readFileSync } from 'node:fs'
const stub = readFileSync(new URL('./wails-stub.js', import.meta.url), 'utf8')

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
