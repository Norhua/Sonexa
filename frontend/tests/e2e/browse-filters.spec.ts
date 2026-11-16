import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'
const rows = (page: Page) => page.locator('.track-list [data-row-key]')
const options = (page: Page) => page.getByRole('button', { name: '视图选项', exact: true })
async function scope(page: Page, label: string) {
  await options(page).click()
  await page.getByRole('menuitemradio', { name: label, exact: true }).click()
}

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    const tracks = [1, 2, 3].map(id => ({ id, title: `歌曲 ${id}`, artist: `艺术家 ${id}`, album: `专辑 ${id}`, releaseId: id + 20, inLibrary: id < 3, addedAt: id < 3 ? '2026-01-01T00:00:00Z' : '' }))
    const filter = (f: any) => tracks.filter(t => (f.includeSynced || t.inLibrary) &&
      (!f.artistId || f.artistId === t.id) && (!f.releaseGroupId || f.releaseGroupId === t.releaseId) &&
      (f.playlistIds == null || f.playlistIds.some((id: number) => id === 11 ? t.id === 1 : t.id === 2)) &&
      (f.tagIds == null || f.tagIds.some((id: number) => id === 31 ? t.id === 1 : t.id === 2)) &&
      (f.syncedPlaylistIds == null || f.syncedPlaylistIds.some((id: number) => id === 71 ? t.id >= 2 : t.id === 1)) &&
      (!f.query || t.title.includes(f.query)))
    api.ListCollections = async (kind: string) => kind === 'tag' ? [{ id: 31, name: '标签 A' }, { id: 32, name: '标签 B' }] : [{ id: 11, name: '歌单 A' }, { id: 12, name: '歌单 B' }]
    api.ListSyncedPlaylists = async () => [{ id: 71, name: '同步 A', importedCount: 3 }, { id: 72, name: '同步 B', importedCount: 1 }]
    api.ListTracks = async (f: any) => { w.__filter = JSON.parse(JSON.stringify(f)); const list = filter(f); return { tracks: list, total: list.length } }
    api.ListArtistsFiltered = async (f: any) => filter(f).map(t => ({ id: t.id, name: t.artist, trackCount: 1 }))
    api.GetArtistHeaderFiltered = async (id: number, f: any) => ({ id, title: `艺术家 ${id}`, facets: [{ role: '', label: '全部', count: filter({ ...f, artistId: id }).length }] })
    api.ListAlbumsFiltered = async (f: any) => filter(f).map(t => ({ releaseId: t.releaseId, groupId: t.releaseId, title: t.album, ownedCount: +t.inLibrary, savedCount: 1, memberCount: 1 }))
    api.GetAlbumGroupDetailForScope = async (id: number, all: boolean) => {
      const t = tracks.find(t => t.releaseId === id)!
      const release = { releaseId: id, title: t.album, ownedCount: +t.inLibrary, savedCount: 1, rows: [{ trackId: t.id, title: t.title, ghost: false, inLibrary: t.inLibrary, addedAt: t.addedAt }] }
      const result = { groupId: id, title: t.album, representativeId: id, ownedCount: +t.inLibrary, savedCount: 1, memberCount: 1, releases: all || t.inLibrary ? [release] : [] }
      if (w.__delayAlbum) return new Promise(resolve => { w.__finishAlbum = () => resolve(result) })
      return result
    }
    api.GetSyncedPlaylistEntriesFiltered = async (_id: number, f: any) => {
      w.__syncFilter = JSON.parse(JSON.stringify(f))
      return [2, 3, 3].filter(id => filter(f).some(t => t.id === id)).map((id, i) => ({ entryId: i + 100, trackId: id, inLibrary: id < 3, upstreamPresent: i < 2, track: tracks[id - 1] }))
    }
    api.AddToCollection = async (...args: any[]) => { w.__collectionWrite = args }
  })
  await page.goto('/')
  await expect(rows(page)).toHaveCount(2)
})

test('page menus, hover, continuous multi-selection, empty selection and all reset', async ({ page }) => {
  await options(page).click()
  await expect(page.getByRole('menuitemradio').allTextContents()).resolves.toEqual(['全部歌曲'])
  await expect(page.getByRole('menuitem').allTextContents()).resolves.toEqual(['歌单', '标签'])
  await page.keyboard.press('Escape')
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await options(page).click()
  await expect(page.getByRole('menuitemradio', { name: '仅在曲库', exact: true })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByRole('menuitemradio', { name: '全部歌曲', exact: true })).toHaveAttribute('aria-checked', 'false')
  const parent = page.getByRole('menuitem', { name: '同步歌单', exact: true })
  await parent.hover()
  await expect(parent).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('menu', { name: '同步歌单', exact: true })).toBeVisible()
  await expect(page.getByRole('menuitemcheckbox', { name: '全选', exact: true })).toHaveAttribute('aria-checked', 'true')
  await page.getByRole('menuitemcheckbox', { name: '同步 B', exact: true }).click()
  await expect(parent.locator('.lucide-circle-dashed')).toHaveCount(1)
  await expect(rows(page)).toHaveCount(1)
  await page.getByRole('menuitemcheckbox', { name: '同步 A', exact: true }).click()
  await expect(rows(page)).toHaveCount(0)
  await page.getByRole('menuitemcheckbox', { name: '全选', exact: true }).click()
  await expect(rows(page)).toHaveCount(2)
  await expect(parent.locator('.lucide-circle-dashed')).toHaveCount(0)
  await page.getByRole('menuitemradio', { name: '全部歌曲', exact: true }).click()
  await expect(rows(page)).toHaveCount(3)
  await options(page).click()
  await page.getByRole('menuitem', { name: '歌单', exact: true }).hover()
  await page.getByRole('menuitemcheckbox', { name: '歌单 B', exact: true }).click()
  await page.getByRole('menuitem', { name: '标签', exact: true }).hover()
  await page.getByRole('menuitemcheckbox', { name: '标签 A', exact: true }).click()
  await expect(rows(page)).toHaveCount(0) // playlist A ∩ tag B
  await page.getByRole('menuitemradio', { name: '全部歌曲', exact: true }).click()
  await expect(rows(page)).toHaveCount(3)
  expect(await page.evaluate(() => (window as any).__filter)).toMatchObject({ recentOnly: true, includeSynced: true, playlistIds: null, tagIds: null, syncedPlaylistIds: null })
})

test('artists and albums show saved-only entities without allowing collection writes', async ({ page }) => {
  for (const kind of ['艺术家', '专辑']) {
    await page.getByRole('button', { name: kind, exact: true }).click()
    await expect(page.getByRole('button', { name: new RegExp(`${kind} 3`) })).toHaveCount(0)
    await scope(page, '全部歌曲')
    await page.getByRole('button', { name: new RegExp(`${kind} 3`) }).click()
    await expect(rows(page)).toHaveCount(1)
    await expect(rows(page)).toHaveAttribute('data-ghost', 'false')
    await rows(page).first().click({ button: 'right' })
    await expect(page.getByRole('menuitem', { name: '添加标签…', exact: true })).toHaveAttribute('aria-disabled', 'true')
    await expect(page.getByRole('menuitem', { name: '添加到歌单…', exact: true })).toHaveAttribute('aria-disabled', 'true')
    await page.keyboard.press('Escape')
    await scope(page, '仅在曲库')
    await expect(rows(page)).toHaveCount(0)
  }
  expect(await page.evaluate(() => (window as any).__collectionWrite)).toBeUndefined()
})

test('synced menu defaults all and intersects membership, tags and query, preserving occurrences', async ({ page }) => {
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await expect(rows(page)).toHaveCount(3)
  await options(page).click()
  await expect(page.getByRole('menuitemradio', { name: '全部歌曲', exact: true })).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByRole('menuitem', { name: '同步歌单', exact: true })).toHaveCount(0)
  await page.getByRole('menuitemradio', { name: '仅在曲库', exact: true }).click()
  await expect(rows(page)).toHaveCount(1)
  await rows(page).click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: '添加标签…', exact: true })).not.toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('Escape')
  await scope(page, '全部歌曲')
  await rows(page).first().click(); await rows(page).first().press('ControlOrMeta+a')
  await rows(page).first().click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: '添加标签…', exact: true })).toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('Escape')
  await options(page).click()
  await page.getByRole('menuitem', { name: '标签', exact: true }).hover()
  await page.getByRole('menuitemcheckbox', { name: '标签 A', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(rows(page)).toHaveCount(1)
  await page.getByRole('searchbox').fill('3')
  await expect(rows(page)).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__syncFilter)).toMatchObject({ includeSynced: true, tagIds: [32], query: '3' })
})

test('menu remains beside its parent in a small desktop window', async ({ page }) => {
  await page.setViewportSize({ width: 960, height: 640 })
  await page.getByRole('button', { name: '最近添加', exact: true }).click()
  await options(page).click()
  const parent = page.getByRole('menuitem', { name: '同步歌单', exact: true })
  await parent.hover()
  const submenu = page.getByRole('menu', { name: '同步歌单', exact: true })
  await expect(submenu).toBeVisible()
  const child = (await submenu.boundingBox())!, root = (await page.locator('[data-pc-name="tieredmenu"]').boundingBox())!
  expect(child.x >= root.x + root.width - 6 || child.x + child.width <= root.x + 6).toBe(true)
  expect(child.y).toBeGreaterThanOrEqual(0)
  expect(child.y + child.height).toBeLessThanOrEqual(640)
  await page.getByRole('menuitemcheckbox', { name: '同步 B', exact: true }).click()
  await expect(rows(page)).toHaveCount(1)
  await page.keyboard.press('Escape')
  await expect(options(page)).toBeFocused()
})

test('late album detail cannot restore saved-only rows after switching back to library scope', async ({ page }) => {
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await scope(page, '全部歌曲')
  await page.evaluate(() => { (window as any).__delayAlbum = true })
  await page.getByRole('button', { name: /专辑 3/ }).click()
  await expect.poll(() => page.evaluate(() => !!(window as any).__finishAlbum)).toBe(true)
  await page.evaluate(() => { (window as any).__delayAlbum = false })
  await scope(page, '仅在曲库')
  await page.evaluate(() => (window as any).__finishAlbum())
  await expect(rows(page)).toHaveCount(0)
  await expect(page.getByRole('button', { name: /专辑 3/ })).toHaveCount(0)
})
