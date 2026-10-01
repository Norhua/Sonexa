import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test('all releases retain collections, tags filter every section and ghost admission has its own release', async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.ListCollections = async (kind: string) => kind === 'tag' ? [{ id: 31, name: '测试标签', kind: 'tag' }] : []
    ;(window as any).runtime.EventsEmit('library:changed')
    const releases = [
      { releaseId: 20, title: '数字版', year: 2014, ownedCount: 1, totalCount: 14, tracklistState: 'complete', provider: 'musicbrainz', releaseRef: 'digital', media: [], rows: [
        { trackId: 1, title: '共有曲数字版', discNumber: 1, trackNumber: 6, ghost: false },
        { trackId: 0, title: '数字版未收藏', discNumber: 1, trackNumber: 7, ghost: true, trackRef: 'digital-7' },
      ] },
      { releaseId: 21, title: 'SACD 版', year: 2012, ownedCount: 2, totalCount: 15, tracklistState: 'complete', provider: 'musicbrainz', releaseRef: 'sacd', media: [], rows: [
        { trackId: 2, title: 'White Love', discNumber: 1, trackNumber: 6, ghost: false },
        { trackId: 3, title: '共有曲 SACD 版', discNumber: 1, trackNumber: 7, ghost: false },
        { trackId: 0, title: 'SACD 未收藏', discNumber: 2, trackNumber: 1, ghost: true, trackRef: 'sacd-15' },
      ] },
    ]
    api.ListAlbums = async () => [{ releaseId: 20, groupId: 10, title: '多版本收藏', ownedCount: 3, memberCount: 2 }]
    api.GetAlbumGroupDetail = async () => ({ groupId: 10, title: '多版本收藏', representativeId: 20, ownedCount: 3, memberCount: 2, releases })
    const list = api.ListTracks
    api.ListTracks = async (filter: any) => {
      const result = await list(filter)
      return { ...result, tracks: result.tracks.filter((t: any) => !filter.tagId || t.id <= 2) }
    }
    api.PrepareGhostAdmission = async (...args: any[]) => { (window as any).__ghost = args; return { id: 'ghost', hash: 'hash', baseline: {} } }; api.CommitAdmission = async () => ({ trackId: 4 })
  })
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /多版本收藏/ }).click()
  const digital = page.locator('[data-release-id="20"]'), sacd = page.locator('[data-release-id="21"]')
  await expect(digital).toContainText('2014')
  await expect(sacd).toContainText('2012')
  await expect(sacd.locator('[data-row-key]')).toHaveCount(2)
  await expect(sacd).toContainText('White Love')
  await sacd.getByRole('button', { name: '展开发行清单' }).click()
  await expect(sacd.locator('[data-row-key]')).toHaveCount(3)
  await sacd.getByText('SACD 未收藏', { exact: true }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '加入曲库', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__ghost)).toEqual([21, 2, 1])
  await page.getByRole('button', { name: '仅显示已有', exact: true }).click()
  await expect(digital.locator('[data-row-key]')).toHaveCount(1)
  await expect(sacd.locator('[data-row-key]')).toHaveCount(2)
  expect(await page.evaluate(() => localStorage.getItem('sonexa.onlyOwned'))).toBe('1')
  await page.getByRole('button', { name: '视图选项', exact: true }).click()
  await page.getByRole('menuitem', { name: '按标签', exact: true }).click()
  await page.getByRole('menuitemradio', { name: /测试标签/ }).click()
  await expect(digital.locator('[data-row-key]')).toHaveCount(1)
  await expect(sacd.locator('[data-row-key]')).toHaveCount(1)
  await expect(sacd).toContainText('White Love')
})
