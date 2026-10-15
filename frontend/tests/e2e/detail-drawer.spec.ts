import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

const labels = ['专辑', '专辑艺术家', '专辑类型', '年份', '时长', '音轨', '碟号', '每碟曲数', '风格', '发行收藏数', '发行总曲数', '国家/地区', '载体']

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
})

for (const sample of [
  { name: 'single disc', disc: 1, track: 1, discs: 1, media: [{ number: 1, trackTotal: 10 }], total: 10, trackText: '1/10', discText: '1/1' },
  { name: 'multiple discs', disc: 2, track: 3, discs: 2, media: [{ number: 1, trackTotal: 10 }, { number: 2, trackTotal: 6 }], total: 16, trackText: '3/6', discText: '2/2' },
  { name: 'unknown totals', disc: 2, track: 3, discs: null, media: [{ number: 1, trackTotal: 10 }, { number: 2, trackTotal: null }], total: 16, trackText: '3/—', discText: '2/—' },
  { name: 'unknown position', disc: null, track: null, discs: 2, media: [{ number: 1, trackTotal: 10 }], total: 16, trackText: '—/—', discText: '—/2' },
]) {
  test(`drawer release order and position totals: ${sample.name}`, async ({ page }, testInfo) => {
    await page.goto('/')
    await page.evaluate(sample => {
      const api = (window as any).go.app.App
      const original = api.GetTrackDetail
      api.GetTrackDetail = async (id: number) => {
        const d = await original(id)
        return {
          ...d,
          track: { ...d.track, title: '(Vocal) you', artist: '霞ヶ丘詩羽(CV.茅野愛衣)' },
          metadata: { year: 2015, durationMs: 243800, discNumber: sample.disc, trackNumber: sample.track, genre: 'J-Pop', mediumFormat: 'CD', country: 'JP' },
          release: { title: '冴えない彼女の育てかた 第4巻 特典CD', credit: 'Various Artists', primaryType: 'Single', discTotal: sample.discs, media: sample.media, ownedCount: 1, totalCount: sample.total },
          participants: [
            { role: 'vocal', label: '演唱', artists: [{ artistId: 2, name: '霞ヶ丘詩羽(CV.茅野愛衣)', source: 'credit_cv' }] },
            { role: 'composer', label: '作曲', artists: [{ artistId: 3, name: 'dai', source: 'musicbrainz' }] },
          ],
        }
      }
    }, sample)
    await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
    const drawer = page.locator('aside').filter({ hasText: '参与者' })
    const release = drawer.locator('section').filter({ has: page.getByRole('heading', { name: '发行信息', exact: true }) })
    await expect(release.locator('dt')).toHaveText(labels)
    const value = (label: string) => release.locator('dt').filter({ hasText: new RegExp(`^${label}$`) }).locator('xpath=following-sibling::dd[1]')
    await expect(value('音轨')).toHaveText(sample.trackText)
    await expect(value('碟号')).toHaveText(sample.discText)
    await expect(value('时长')).toHaveText('4:04')
    await expect(value('发行总曲数')).toHaveText(String(sample.total))
    await expect(value('发行收藏数')).toHaveText('1')
    await expect(value('每碟曲数')).toHaveText(sample.media.map(m => `第 ${m.number} 碟 ${m.trackTotal ?? '—'}`).join(' · '))
    const participants = drawer.locator('section').filter({ has: page.getByRole('heading', { name: '参与者', exact: true }) })
    await expect(participants.locator('dt')).toHaveText(['演唱', '作曲'])
    await expect(participants.getByRole('button', { name: '霞ヶ丘詩羽(CV.茅野愛衣)', exact: true })).toBeVisible()
    await expect(participants.getByRole('button', { name: '茅野愛衣', exact: true })).toHaveCount(0)
    await expect(participants.getByText('来自 CV 署名', { exact: false })).toHaveCount(0)
    if (sample.name === 'single disc') await page.screenshot({ path: testInfo.outputPath('cv-detail.png') })
  })
}

test('whole CV link and both artist entries show the same song', async ({ page }, testInfo) => {
  await page.goto('/')
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const credit = '霞ヶ丘詩羽(CV.茅野愛衣)'
    const song = { id: 7, title: '(Vocal) you', artist: credit }
    const artists = [{ id: 30, name: credit, trackCount: 1 }, { id: 31, name: '茅野愛衣', trackCount: 1 }]
    api.ListArtists = async () => artists
    api.GetArtistHeader = async (id: number) => ({ id, title: artists.find(a => a.id === id)?.name, facets: [{ role: '', label: '全部', count: 1 }] })
    api.ListTracks = async () => ({ tracks: [song], total: 1 })
    api.GetTrackDetail = async () => ({ track: song, creditText: credit, creditSpans: [], tags: [], participants: [
      { role: 'vocal', label: '演唱', artists: [{ artistId: 30, name: credit, source: 'credit_cv' }] },
    ] })
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const participants = page.locator('aside section').filter({ has: page.getByRole('heading', { name: '参与者', exact: true }) })
  await participants.getByRole('button', { name: '霞ヶ丘詩羽(CV.茅野愛衣)', exact: true }).click()
  await expect(page.getByRole('heading', { name: '艺术家', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '霞ヶ丘詩羽(CV.茅野愛衣) 1', exact: true })).toHaveAttribute('aria-current', 'true')
  for (const name of ['霞ヶ丘詩羽(CV.茅野愛衣)', '茅野愛衣']) {
    const entry = page.getByRole('button', { name: `${name} 1`, exact: true })
    await expect(entry).toBeVisible()
    await entry.click()
    await expect(entry).toHaveAttribute('aria-current', 'true')
    await expect(page.getByRole('cell', { name: '(Vocal) you', exact: true })).toBeVisible()
  }
  await page.screenshot({ path: testInfo.outputPath('cv-artists.png') })
})

test('drawer without release or metadata keeps placeholders', async ({ page }) => {
  await page.goto('/')
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  const drawer = page.locator('aside').filter({ hasText: '参与者' })
  const release = drawer.locator('section').filter({ has: page.getByRole('heading', { name: '发行信息', exact: true }) })
  await expect(release.locator('dt')).toHaveText(labels)
  await expect(release.locator('dd')).toHaveText(['—', '—', '—', '—', '—', '—/—', '—/—', '—', '—', '—', '—', '—', '—'])
  await expect(drawer.getByText('来自 CV 署名', { exact: false })).toHaveCount(0)
})
