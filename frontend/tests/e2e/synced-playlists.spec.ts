import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.addInitScript(() => {
    const w = window as any, api = w.go.app.App
    const state = w.__sync = { bindings: [
      { id: 71, name: '同步 A', importedCount: 3, revision: 1 },
      { id: 72, name: '同步 B', importedCount: 1, revision: 1 },
    ], member: false, commits: 0, cancels: [] as string[], restores: [] as number[], fail: '', existing: false }
    const track = (id: number) => ({ id, title: id === 1 ? '共享歌曲' : '保留歌曲', artist: '署名', album: '专辑', inLibrary: state.member, addedAt: '' })
    api.ListSyncedPlaylists = async () => structuredClone(state.bindings)
    api.ListPlaylistSources = async () => [{ id: 'stub', name: '只读替身', available: true }, { id: 'unavailable', name: '环境缺失', available: false, reason: 'helper 不可用' }]
    api.GetSyncedPlaylistEntries = async (id: number) => structuredClone((id === 71 ? [101, 102, 103] : [201]).map((entryId, i) => ({ entryId, trackId: i < 2 ? 1 : 2, upstreamPresent: i !== 2, inLibrary: state.member, track: track(i < 2 ? 1 : 2) })))
    const diff = () => ({ added: [], removed: [{ entryId: 103, entry: { entryKey: 'removed', title: '保留歌曲', artists: ['署名'] }, disposition: 'retain_absent' }], restored: [], ignored: [501, 502].map(id => ({ entryId: id, entry: { entryKey: String(id), title: `忽略歌曲 ${id}`, artists: [] }, disposition: 'keep_ignored' })), excluded: [] })
    api.PreparePlaylistSync = async (_op: string, id: number) => {
      if (state.fail) throw new Error(state.fail)
      return { id: `plan-${id}`, hash: 'hash', playlistId: id, name: `同步 ${id}`, alias: '', expiresAt: '2099-01-01T00:00:00Z', difference: diff() }
    }
    api.PreparePlaylistBinding = async () => state.existing ? { existingPlaylistId: 71, preview: null } : { existingPlaylistId: 0, preview: { id: 'bind', hash: 'hash', name: '新歌单', alias: '', difference: diff() } }
    api.CommitPlaylistBinding = async () => { state.commits++; return { playlistId: 71 } }
    api.CommitPlaylistSync = async (_id: string, _hash: string, restore: number[]) => { state.commits++; state.restores = restore; w.runtime.EventsEmit('synced-playlists:changed', { all: true, playlists: [] }); return { playlistId: 71 } }
    api.CancelPlaylistOperation = async (id: string) => { state.cancels.push(id) }
    api.PrepareLibraryAddition = async (ids: number[]) => ({ id: 'add', hash: 'hash', willAdd: state.member ? [] : ids, alreadyInLibrary: state.member ? ids : [] })
    api.CommitLibraryAddition = async () => { state.member = true; w.runtime.EventsEmit('library:changed'); w.runtime.EventsEmit('synced-playlists:changed', { all: true, playlists: [] }); return { willAdd: [1, 2], alreadyInLibrary: [] } }
    api.DeleteSyncedPlaylist = async (id: number) => { state.bindings = state.bindings.filter((p: any) => p.id !== id); w.runtime.EventsEmit('synced-playlists:changed', { all: true, playlists: [] }) }
  })
  await page.goto('/')
  await expect(page.locator('nav').getByRole('button', { name: /同步 A/ })).toBeVisible()
})

test('independent navigation, occurrence rows, readonly menu and missing-item admission', async ({ page }) => {
  const groups = await page.locator('nav h2').allTextContents()
  expect(groups).toEqual(['资料库', '歌单', '同步歌单', '标签', '任务'])
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  const rows = page.locator('.track-list [data-row-key]')
  await expect(rows).toHaveCount(3)
  await expect(page.getByRole('status')).toContainText('3 个已导入项')
  const absent = page.locator('[data-upstream-missing="true"]')
  await expect(absent).toHaveAttribute('data-ghost', 'false')
  await absent.click(); await absent.press('Delete')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await expect(rows).toHaveCount(3)
  await absent.dblclick()
  await expect(page.locator('[role="dialog"]').filter({ hasText: '添加到我的音乐库' })).toHaveCount(0)
  await absent.click(); await absent.press('ControlOrMeta+a')
  await absent.click({ button: 'right' })
  await expect(page.getByRole('menuitem', { name: '删除', exact: true })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: /导出/ })).toHaveCount(0)
  await expect(page.getByRole('menuitem', { name: /添加到歌单/ })).toHaveAttribute('aria-disabled', 'true')
  await expect(page.getByRole('menuitem', { name: /添加标签/ })).toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('menuitem', { name: '添加到我的音乐库', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '添加到我的音乐库' })
  await expect(dialog).toContainText('将添加（2）')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__sync.member)).toBe(false)
  await absent.click({ button: 'right' }); await page.getByRole('menuitem', { name: '添加到我的音乐库', exact: true }).click()
  await dialog.getByRole('button', { name: '确认添加' }).click()
  await page.locator('.track-list thead th').first().click({ button: 'right' })
  await page.getByRole('menuitemcheckbox', { name: '已入库', exact: true }).click()
  await expect(page.locator('.track-list [aria-label="已入库"]')).toHaveCount(3)
  await expect(absent).toHaveCount(1)
  await page.locator('nav').getByRole('button', { name: /同步 B/ }).click()
  await expect(page.locator('.track-list [aria-label="已入库"]')).toHaveCount(1)
})

test('difference preview is cancellable and ignored restoration is explicit', async ({ page }) => {
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  const dialog = page.getByRole('dialog', { name: '确认同步差异' })
  await expect(dialog).toContainText('保留已导入曲目并置灰')
  await expect(dialog.getByText('忽略歌曲 501', { exact: true })).not.toBeVisible()
  expect(await page.evaluate(() => (window as any).__sync.commits)).toBe(0)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__sync.commits)).toBe(0)
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  await dialog.locator('summary').click()
  await dialog.getByRole('checkbox', { name: /忽略歌曲 501/ }).check()
  await dialog.getByRole('button', { name: /确认同步/ }).click()
  expect(await page.evaluate(() => (window as any).__sync.restores)).toEqual([501])
  await page.evaluate(() => { (window as any).__sync.fail = '读取不完整' })
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  await expect(page.getByText('Error: 读取不完整', { exact: true })).toBeVisible()
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(3)
})

test('identified filtered songs show reasons without fabricated positions in both previews', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    const excluded = [102182145, 101041088, 214078036].map(id => ({ position: 0, trackRef: String(id), kind: 'unavailable_track', reason: 'QQ 未返回该歌曲详情；已有同步条目保持不变' }))
    const sync = api.PreparePlaylistSync, bind = api.PreparePlaylistBinding
    api.PreparePlaylistSync = async (...args: any[]) => { const p = await sync(...args); p.difference.excluded = excluded; return p }
    api.PreparePlaylistBinding = async (...args: any[]) => { const p = await bind(...args); p.preview.difference.excluded = excluded; return p }
  })
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  const sync = page.getByRole('dialog', { name: '确认同步差异' })
  await expect(sync).toContainText('明确排除（3）')
  await expect(sync.getByText(/原始位置未知 ·/)).toHaveCount(3)
  await expect(sync).not.toContainText('位置 0')
  await sync.getByRole('button', { name: '取消', exact: true }).click()
  await expect(sync).not.toBeVisible()
  await expect(page.locator('[data-pc-section="mask"]')).toHaveCount(0)
  const open = page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true })
  await open.focus(); await open.click()
  const bind = page.getByRole('dialog', { name: '绑定同步歌单' })
  await bind.getByLabel('歌单链接或 ID', { exact: true }).fill('list')
  await bind.getByRole('button', { name: '读取歌单', exact: true }).click()
  await expect(bind.getByTestId('binding-preview')).toContainText('明确排除 3 项')
  await expect(bind.getByTestId('binding-preview').getByRole('listitem')).toHaveCount(3)
  await expect(bind).toContainText('102182145 · QQ 未返回该歌曲详情；已有同步条目保持不变')
})

test('binding uses capability list, shows unavailable runtime, and locates duplicate', async ({ page }) => {
  await page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true }).focus()
  await page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '绑定同步歌单' })
  await dialog.getByLabel('歌单链接或 ID', { exact: true }).fill('list')
  await dialog.getByRole('button', { name: '读取歌单', exact: true }).click()
  await expect(dialog.getByTestId('binding-preview')).toContainText('新歌单')
  expect(await page.evaluate(() => (window as any).__sync.commits)).toBe(0)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await expect(page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true })).toBeFocused()
  await page.evaluate(() => { (window as any).__sync.existing = true })
  await page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true }).click()
  await dialog.getByLabel('歌单链接或 ID', { exact: true }).fill('list')
  await dialog.getByRole('button', { name: '读取歌单', exact: true }).click()
  await dialog.getByRole('button', { name: '打开已绑定歌单' }).click()
  await expect(page.locator('main h1')).toHaveText('同步 A')
})

test('late refresh cannot reopen a dialog after switching playlists', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    const original = w.go.app.App.PreparePlaylistSync
    w.go.app.App.PreparePlaylistSync = async (...args: any[]) => new Promise(resolve => { w.__finishSync = async () => resolve(await original(...args)) })
  })
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  await page.locator('nav').getByRole('button', { name: /同步 B/ }).click()
  await page.evaluate(() => (window as any).__finishSync())
  await expect(page.locator('main h1')).toHaveText('同步 B')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(1)
  await expect(page.getByRole('dialog')).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__sync.cancels.length)).toBeGreaterThan(0)
})

test('sources stay backend-driven; unavailable runtime and late platform result are isolated', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, original = w.go.app.App.PreparePlaylistBinding
    w.go.app.App.PreparePlaylistBinding = (...args: any[]) => new Promise(resolve => { w.__finishBind = async () => resolve(await original(...args)) })
  })
  const open = page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true })
  await open.focus(); await open.click()
  const dialog = page.getByRole('dialog', { name: '绑定同步歌单' })
  await dialog.getByLabel('歌单链接或 ID', { exact: true }).fill('id')
  await dialog.getByRole('button', { name: '读取歌单', exact: true }).click()
  await dialog.getByRole('combobox').click()
  await page.getByRole('option', { name: '环境缺失', exact: true }).click()
  await expect(dialog).toContainText('helper 不可用')
  await page.evaluate(() => (window as any).__finishBind())
  await expect(dialog.getByTestId('binding-preview')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '确认绑定' })).toBeDisabled()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(dialog).not.toBeVisible()
  await page.evaluate(() => { (window as any).go.app.App.ListPlaylistSources = async () => [] })
  await open.focus(); await open.click()
  await expect(dialog).toContainText('没有可用的歌单来源')
  await expect(dialog.getByRole('button', { name: '读取歌单', exact: true })).toBeDisabled()
})

test('empty imported list and disabled outbound retain local navigation', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.GetSyncedPlaylistEntries = async () => []
    w.go.app.App.CheckOutbound = async () => ({ allowed: false, reason: '出网已关闭', settingsAnchor: 'upstream' })
    w.runtime.EventsEmit('settings:saved')
  })
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await expect(page.getByText('还没有已导入曲目；待复核、失败和已忽略项不在此显示。')).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新同步歌单' })).toBeDisabled()
})

test('long ignored list scrolls completely and stale commit keeps confirmed rows unchanged', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, original = w.go.app.App.PreparePlaylistSync
    w.go.app.App.PreparePlaylistSync = async (...args: any[]) => {
      const p = await original(...args)
      p.difference.removed = []
      p.difference.ignored = Array.from({ length: 150 }, (_, i) => ({ entryId: i + 1, entry: { entryKey: String(i), title: `待恢复 ${i + 1}`, artists: [] } }))
      return p
    }
    w.go.app.App.CommitPlaylistSync = async () => { throw new Error('playlist_plan_stale') }
  })
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await page.getByRole('button', { name: '刷新同步歌单' }).click()
  const dialog = page.getByRole('dialog', { name: '确认同步差异' })
  await dialog.locator('summary').click()
  const last = dialog.getByRole('checkbox', { name: /待恢复 150 / })
  await last.scrollIntoViewIfNeeded(); await last.check()
  expect(await dialog.getByTestId('sync-difference').evaluate(el => el.scrollTop)).toBeGreaterThan(0)
  expect(await dialog.getByTestId('sync-difference').evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
  await dialog.getByRole('button', { name: /确认同步/ }).click()
  await expect(dialog).toContainText('playlist_plan_stale')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(3)
})

test('review target hints preserve multiple identities and long-name access', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, original = w.go.app.App.GetReviewQueue
    w.go.app.App.GetReviewQueue = async () => {
      const queue = await original()
      queue.items[0].importDestinations = [
        { kind: 'synced_playlist', playlistId: 71, displayName: '同名歌单' },
        { kind: 'synced_playlist', playlistId: 72, displayName: '同名歌单' },
        { kind: 'synced_playlist', playlistId: 73, displayName: '很长的别名'.repeat(20) },
      ]
      queue.items[1].importDestinations = [{ kind: 'library', displayName: '音乐库' }]
      return queue
    }
  })
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const targets = page.locator('[aria-label="导入目标"]')
  await expect(targets.getByText('to 同名歌单', { exact: true })).toHaveCount(2)
  await expect(targets.locator('[title]').last()).toHaveAttribute('title', `to ${'很长的别名'.repeat(20)}`)
  await expect(targets.locator('span').first()).toHaveClass(/text-orange/)
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(targets).toHaveText('to 音乐库')
})

test('search local uncollected hit explicitly adds membership instead of navigating', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.SearchUpstream = async () => {
      const result = {} as any
      const row = { provider: 'musicbrainz', matchRef: 'local', localTrackId: 1, inLibrary: false, title: '未入库候选', badges: ['本地', 'musicbrainz'], action: '加入曲库', confidence: 100 }
      result.aggregate = [row]
      result.byProvider = [{ provider: 'musicbrainz', displayName: 'MusicBrainz', rows: [row], capabilities: [] }]
      return result
    }
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.locator('main').getByRole('combobox').first().click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '添加到我的音乐库' })).toContainText('未入库候选')
})

test('binding confirmation writes once and closing a pending read never restores its preview', async ({ page }) => {
  const open = page.locator('nav').getByRole('button', { name: '绑定同步歌单', exact: true })
  await open.focus(); await open.click()
  const dialog = page.getByRole('dialog', { name: '绑定同步歌单' })
  await dialog.getByLabel('歌单链接或 ID', { exact: true }).fill('id')
  await dialog.getByRole('button', { name: '读取歌单', exact: true }).click()
  await dialog.getByRole('button', { name: '确认绑定' }).click()
  await expect(dialog).not.toBeVisible()
  expect(await page.evaluate(() => (window as any).__sync.commits)).toBe(1)
  await page.evaluate(() => {
    const w = window as any, original = w.go.app.App.PreparePlaylistBinding
    w.go.app.App.PreparePlaylistBinding = (...args: any[]) => new Promise(resolve => { w.__finishBind = async () => resolve(await original(...args)) })
  })
  await open.focus(); await open.click()
  await dialog.getByLabel('歌单链接或 ID', { exact: true }).fill('late')
  await dialog.getByRole('button', { name: '读取歌单', exact: true }).click()
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.evaluate(() => (window as any).__finishBind())
  await expect(dialog).not.toBeVisible()
  expect(await page.evaluate(() => (window as any).__sync.commits)).toBe(1)
})

test('mixed admission rechecks concurrently collected identities and persists status column', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.go.app.App.PrepareLibraryAddition = async (ids: number[]) => ({ id: 'mixed', hash: 'hash', willAdd: ids.filter(id => id === 2), alreadyInLibrary: ids.filter(id => id === 1) })
    w.go.app.App.CommitLibraryAddition = async () => {
      w.__sync.member = true
      w.runtime.EventsEmit('synced-playlists:changed', { all: true, playlists: [] })
      return { willAdd: [], alreadyInLibrary: [1, 2] }
    }
  })
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  const first = page.locator('.track-list [data-row-key]').first()
  await first.click(); await first.press('ControlOrMeta+a'); await first.click({ button: 'right' })
  await page.getByRole('menuitem', { name: '添加到我的音乐库', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '添加到我的音乐库' })
  await expect(dialog).toContainText('将添加（1）')
  await expect(dialog).toContainText('已在音乐库（1）')
  await dialog.getByRole('button', { name: '确认添加' }).click()
  await page.locator('.track-list thead th').first().click({ button: 'right' })
  await page.getByRole('menuitemcheckbox', { name: '已入库', exact: true }).click()
  await expect(page.locator('.track-list [aria-label="已入库"]')).toHaveCount(3)
  const state = await page.evaluate(() => localStorage.getItem('sonexa.trackColumns.syncedPlaylist'))
  expect(JSON.parse(state!)).toContain('inLibrary')
  await page.reload()
  await page.locator('nav').getByRole('button', { name: /同步 A/ }).click()
  await expect(page.locator('.track-list [aria-label="未入库"]')).toHaveCount(3)
  await expect(page.locator('.track-list thead th').first()).toHaveText('')
})

test('revoked review destinations and batch hints refresh on queue events', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, original = w.go.app.App.GetReviewQueue, batches = w.go.app.App.GetImportQueue
    w.__targets = [{ kind: 'synced_playlist', playlistId: 71, displayName: 'A' }, { kind: 'synced_playlist', playlistId: 72, displayName: 'B' }]
    w.go.app.App.GetReviewQueue = async () => {
      const queue = await original()
      queue.items[0].importDestinations = structuredClone(w.__targets)
      return queue
    }
    w.go.app.App.GetImportQueue = async () => (await batches()).map((b: any) => ({ ...b, destinationKind: 'synced_playlist', destinationName: '批次目标 A' }))
  })
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  const targets = page.locator('[aria-label="导入目标"]')
  await expect(targets).toContainText('to A')
  await expect(targets).toContainText('to B')
  await page.evaluate(() => {
    const w = window as any; w.__targets = w.__targets.slice(1); w.runtime.EventsEmit('import:queue-changed')
  })
  await expect(targets).toHaveText('to B')
  await page.locator('nav').getByRole('button', { name: /导入队列/ }).click()
  await expect(page.locator('[aria-label="导入目标"]').first()).toContainText('to 批次目标 A')
})

test('deleting current binding leaves invalid context', async ({ page }) => {
  const nav = page.locator('nav').getByRole('button', { name: /同步 A/ })
  await nav.click(); await nav.click({ button: 'right' })
  await page.getByRole('menuitem', { name: '删除绑定', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: '删除绑定', exact: true }).click()
  await expect(nav).toHaveCount(0)
  await expect(page.locator('main')).not.toContainText('还没有已导入曲目')
  await expect(page.locator('nav').getByRole('button', { name: '全部曲目', exact: true })).toHaveClass(/bg-highlight/)
})
