import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
})

test('artist list and facets update after library changes while selection and role stay intact', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__artist = { name: '艺术家 A', count: 1, facets: [{ role: '', label: '全部', count: 1 }, { role: 'vocal', label: '演唱', count: 1 }] }
    api.ListArtists = async () => [{ id: 30, name: w.__artist.name, trackCount: w.__artist.count }]
    api.GetArtistHeader = async () => structuredClone({ title: w.__artist.name, facets: w.__artist.facets })
    api.ListTracks = async (filter: any) => { w.__filter = filter; return { tracks: [{ id: 1, title: '歌曲' }], total: 1 } }
  })
  await page.locator('nav').getByRole('button', { name: '艺术家', exact: true }).click()
  await page.getByRole('button', { name: '艺术家 A 1', exact: true }).click()
  await page.getByRole('button', { name: '演唱 1', exact: true }).click()
  await page.getByPlaceholder('在艺术家中查找').fill('艺术家')
  await page.evaluate(() => {
    const w = window as any
    w.__artist = { name: '艺术家新名称', count: 2, facets: [{ role: '', label: '全部', count: 2 }, { role: 'vocal', label: '演唱', count: 2 }, { role: 'composer', label: '作曲', count: 1 }] }
    w.runtime.EventsEmit('library:changed')
  })
  await expect(page.getByRole('button', { name: '艺术家新名称 2', exact: true })).toHaveAttribute('aria-current', 'true')
  await expect(page.getByRole('button', { name: '演唱 2', exact: true })).toHaveClass(/bg-highlight/)
  await expect(page.getByRole('button', { name: '作曲 1', exact: true })).toBeVisible()
  await expect(page.getByPlaceholder('在艺术家中查找')).toHaveValue('艺术家')
  expect(await page.evaluate(() => (window as any).__filter)).toMatchObject({ artistId: 30, role: 'vocal' })
})

test('library search refreshes submitted criteria without replacing the input draft, including while hidden', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.__search = { removed: false, reads: [] }
    w.go.app.App.ListTracks = async (filter: any) => {
      w.__search.reads.push(structuredClone(filter))
      return { tracks: w.__search.removed ? [] : [{ id: 1, title: '命中结果' }], total: w.__search.removed ? 0 : 1 }
    }
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByPlaceholder('搜索资料库').fill('已提交条件')
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.getByRole('cell', { name: '命中结果', exact: true })).toBeVisible()
  await page.getByPlaceholder('搜索资料库').fill('尚未提交草稿')
  await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
  await page.evaluate(() => { const w = window as any; w.__search.removed = true; w.runtime.EventsEmit('library:changed') })
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(0)
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.getByText('资料库里没有匹配的曲目', { exact: true })).toBeVisible()
  await expect(page.getByPlaceholder('搜索资料库')).toHaveValue('尚未提交草稿')
  const reads = await page.evaluate(() => (window as any).__search.reads)
  expect(reads.filter((f: any) => f.query === '已提交条件')).toHaveLength(2)
  expect(reads.some((f: any) => f.query === '尚未提交草稿')).toBe(false)
})

test('all upstream candidate badges refresh locally after additions and deletions without another search', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__search = { owned: false, queries: 0, refreshes: 0 }
    api.SearchUpstream = async () => {
      w.__search.queries++
      return { byProvider: [{ rows: [1, 2].map(id => ({ title: `上游候选 ${id}`, provider: 'qq', matchRef: `ref-${id}`, action: '加入曲库', badges: ['qq'], localTrackId: 0 })), capabilities: [] }] }
    }
    api.RefreshSearchCandidates = async (rows: any[]) => {
      w.__search.refreshes++
      return rows.map(row => ({ ...row, localTrackId: w.__search.owned ? 42 : 0, action: w.__search.owned ? '已在库中' : '加入曲库', badges: w.__search.owned ? ['本地', 'qq'] : ['qq'] }))
    }
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'QQ 音乐', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await expect(page.getByRole('button', { name: '加入曲库', exact: true })).toHaveCount(2)
  for (const owned of [true, false]) {
    await page.evaluate(owned => { const w = window as any; w.__search.owned = owned; w.runtime.EventsEmit('library:changed') }, owned)
    await expect(page.getByRole('button', { name: owned ? /^查看库中曲目：/ : '加入曲库', exact: !owned })).toHaveCount(2)
  }
  expect(await page.evaluate(() => (window as any).__search.queries)).toBe(1)
})

test('unchanged processed counts still update completion, pause, failures and batch identity', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__batch = { id: 1, filename: 'first.csv', status: 'running', running: true, processed: 10, totalRows: 10, fileRows: 10, stats: { imported: 7, review: 1, failed: 2, queued: 0 }, failedTitles: ['失败歌曲'] }
    api.GetImportQueue = async () => [structuredClone(w.__batch)]
    api.GetTaskSummary = async () => ({ activeBatch: w.__batch.id, filename: w.__batch.filename, processed: 10, total: 10, stats: structuredClone(w.__batch.stats), running: w.__batch.running, reviewPending: 8 })
  })
  await page.locator('nav').getByRole('button', { name: /导入队列/ }).click()
  await expect(page.getByText('进行中 · 10/10', { exact: true })).toBeVisible()
  for (const status of ['done', 'paused', 'running']) {
    await page.evaluate(status => {
      const w = window as any
      w.__batch.status = status; w.__batch.running = status === 'running'
      if (status === 'running') { w.__batch.id = 2; w.__batch.filename = 'second.csv'; w.__batch.failedTitles = [] }
      w.runtime.EventsEmit('import:progress', { batchId: w.__batch.id, processed: 10, total: 10, stats: w.__batch.stats, done: status === 'done', paused: status === 'paused', line: '' })
    }, status)
    await expect(page.getByText(`${{ done: '已完成', paused: '已暂停', running: '进行中' }[status]} · 10/10`, { exact: true })).toBeVisible()
    await expect(page.locator('nav').getByRole('button', { name: /复核队列/ })).toHaveText(/复核队列\s*8$/)
  }
  await expect(page.getByText('second.csv', { exact: true })).toBeVisible()
  await expect(page.getByText('失败歌曲', { exact: true })).toHaveCount(0)
})

test('import queue coalesces progress bursts and never applies older responses after latest state', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__pending = []; w.__reads = 0
    api.GetImportQueue = async () => {
      w.__reads++
      return new Promise(resolve => w.__pending.push((title: string) => resolve([{ id: 1, filename: title, status: 'running', running: true, processed: 1, totalRows: 10, fileRows: 10, stats: { imported: 1 }, failedTitles: [] }])))
    }
  })
  await page.locator('nav').getByRole('button', { name: /导入队列/ }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__reads)).toBe(1)
  await page.evaluate(() => {
    const w = window as any
    for (let i = 0; i < 10; i++) w.runtime.EventsEmit('import:progress', { batchId: 1, processed: i, total: 10, stats: {}, done: false, paused: false })
  })
  expect(await page.evaluate(() => (window as any).__reads)).toBe(1)
  await page.evaluate(() => (window as any).__pending[0]('旧批次.csv'))
  await expect.poll(() => page.evaluate(() => (window as any).__reads)).toBe(2)
  await expect(page.getByText('旧批次.csv', { exact: true })).toHaveCount(0)
  await page.evaluate(() => (window as any).__pending[1]('最新批次.csv'))
  await expect(page.getByText('最新批次.csv', { exact: true })).toBeVisible()
})

test('committed background changes refresh visible library, drawer and album without reentry', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__stage = 0
    const song = () => ({ id: 1, title: w.__stage ? '补全后曲名' : '原曲名', releaseId: 20 })
    api.ListTracks = async () => ({ tracks: [song(), ...(w.__stage ? [{ id: 2, title: '导入新增曲目' }] : [])], total: w.__stage ? 2 : 1 })
    api.GetTrackDetail = async () => ({ track: song(), participants: [], creditSpans: [], tags: [], metadata: { year: w.__stage ? 2026 : 2000 } })
    api.ListAlbums = async () => [{ releaseId: 20, groupId: 20, title: w.__stage === 2 ? '补全后专辑' : '原专辑', ownedCount: w.__stage === 2 ? 2 : 1 }]
    api.GetAlbumGroupDetail = async () => ({ groupId: 20, title: w.__stage === 2 ? '补全后专辑' : '原专辑', representativeId: 20, releases: [{ releaseId: 20, title: w.__stage === 2 ? '补全后专辑' : '原专辑', rows: [{ trackId: 1, title: w.__stage === 2 ? '专辑已更新歌曲' : '原歌曲' }] }] })
  })
  await page.locator('.track-list [data-row-key]').first().locator('td').last().dblclick()
  await page.evaluate(() => { const w = window as any; w.__stage = 1; w.runtime.EventsEmit('library:changed') })
  await expect(page.getByRole('cell', { name: '导入新增曲目', exact: true })).toBeVisible()
  await expect(page.locator('aside h2')).toHaveText('补全后曲名')
  await page.locator('nav').getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: /原专辑/ }).click()
  await expect(page.getByRole('cell', { name: '原歌曲', exact: true })).toBeVisible()
  await page.evaluate(() => { const w = window as any; w.__stage = 2; w.runtime.EventsEmit('library:changed') })
  await expect(page.getByRole('button', { name: /补全后专辑/ })).toBeVisible()
  await expect(page.getByRole('cell', { name: '专辑已更新歌曲', exact: true })).toBeVisible()
})
