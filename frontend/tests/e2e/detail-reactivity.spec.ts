import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

const drawerSelector = 'aside'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(async () => {
    const w = window as any, api = w.go.app.App
    const list = await api.ListTracks({})
    // 读接口始终返回独立副本，避免修改后端桩件对象直接触发 Vue。
    w.__details = Object.fromEntries(list.tracks.map((track: any) => [track.id, {
      track: { ...track, releaseId: 20, coverRevision: 0 },
      release: { title: '原专辑', credit: '原专辑艺术家', media: [], ownedCount: 7 },
      metadata: {}, participants: [], creditSpans: [], tags: [],
    }]))
    w.__detailReads = []
    api.GetTrackDetail = async (id: number) => {
      w.__detailReads.push(id)
      return structuredClone(w.__details[id])
    }
    api.ListTracks = async () => ({ tracks: Object.values(w.__details).map((d: any) => structuredClone(d.track)), total: 7 })
    const save = api.SaveEdit
    api.SaveEdit = async (...args: any[]) => {
      const result = await save(...args)
      const draft = w.__editDraft, d = w.__details[draft.trackId]
      const title = draft.fields['track.title']
      if (title) d.track.title = JSON.parse(title.valueJson)
      const album = draft.fields['release.title']
      if (album) for (const detail of Object.values(w.__details) as any[]) detail.release.title = JSON.parse(album.valueJson)
      if (w.__emitSave) w.runtime.EventsEmit('library:changed')
      return result
    }
  })
})

for (const emitSave of [false, true]) {
  test(`saved editor immediately refreshes open detail (library event: ${emitSave})`, async ({ page }) => {
    await page.evaluate(value => { (window as any).__emitSave = value }, emitSave)
    await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
    const drawer = page.locator(drawerSelector).filter({ hasText: '参与者' })
    await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
    await drawer.getByRole('button', { name: '编辑', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
    await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('保存后新曲名')
    await editor.getByRole('button', { name: '保存', exact: true }).click()
    await expect(editor).toHaveCount(0)
    await expect(drawer.locator('h2')).toHaveText('保存后新曲名')
    await expect(page.locator('.track-list [data-row-key="track:1"]')).toContainText('保存后新曲名')
  })
}

test('editing another song refreshes the shared album in the currently open drawer', async ({ page }) => {
  await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
  const drawer = page.locator(drawerSelector).filter({ hasText: '参与者' })
  await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
  await page.locator('.track-list [data-row-key="track:2"]').click({ button: 'right' })
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
  await editor.getByRole('textbox', { name: '专辑名', exact: true }).fill('共享专辑新名称')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await page.getByRole('button', { name: '修改专辑信息', exact: true }).click()
  await expect(editor).toHaveCount(0)
  await expect(drawer).toContainText('共享专辑新名称')
  await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
  expect(await page.evaluate(() => (window as any).__editDraft.trackId)).toBe(2)
})

test('library notifications refresh all detail sections without reopening', async ({ page }) => {
  await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
  const drawer = page.locator(drawerSelector).filter({ hasText: '参与者' })
  await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
  await page.evaluate(() => {
    const w = window as any, d = w.__details[1]
    d.track.title = '后台更新曲名'
    d.track.artist = '更新署名'
    d.release = { title: '更新发行', credit: '更新专辑艺术家', discTotal: 2, media: [{ number: 2, trackTotal: 8 }], ownedCount: 3, totalCount: 16 }
    d.metadata = { year: 2026, discNumber: 2, trackNumber: 4, lyrics: '更新歌词' }
    d.participants = [{ role: 'vocal', label: '演唱', artists: [{ artistId: 30, name: '更新演唱者' }] }]
    d.tags = [{ id: 40, name: '新标签' }]
    w.runtime.EventsEmit('library:changed')
  })
  await expect(drawer.locator('h2')).toHaveText('后台更新曲名')
  for (const text of ['更新署名', '更新发行', '更新专辑艺术家', '2026', '4/8', '2/2', '更新演唱者', '#新标签']) {
    await expect(drawer.getByText(text, { exact: true })).toBeVisible()
  }
  await drawer.getByRole('button', { name: '查看歌词（已有歌词）' }).click()
  await expect(page.getByLabel('歌曲歌词').getByText('更新歌词', { exact: true })).toBeVisible()
})

test('background refresh targets pending selection B, not still displayed A, and ignores older replies', async ({ page }) => {
  await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
  const drawer = page.locator(drawerSelector).filter({ hasText: '参与者' })
  await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, get = api.GetTrackDetail
    w.__pendingDetails = []
    api.GetTrackDetail = async (id: number) => {
      const snapshot = await get(id)
      return new Promise(resolve => w.__pendingDetails.push({ id, finish: () => resolve(snapshot) }))
    }
  })
  await page.locator('.track-list [data-row-key="track:2"] td').last().click()
  await expect.poll(() => page.evaluate(() => (window as any).__pendingDetails.length)).toBe(1)
  await page.evaluate(() => {
    const w = window as any
    w.__details[2].track.title = 'B 的最新详情'
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => (window as any).__pendingDetails.map((p: any) => p.id))).toEqual([2, 2])
  await page.evaluate(() => (window as any).__pendingDetails[1].finish())
  await expect(drawer.locator('h2')).toHaveText('B 的最新详情')
  await page.evaluate(() => (window as any).__pendingDetails[0].finish())
  await expect(drawer.locator('h2')).toHaveText('B 的最新详情')
})

test('closing during background refresh prevents late replies and later events from reopening the drawer', async ({ page }) => {
  await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
  const drawer = page.locator(drawerSelector).filter({ hasText: '参与者' })
  await expect(drawer.locator('h2')).toHaveText('綿雪の夢')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, get = api.GetTrackDetail
    api.GetTrackDetail = async (id: number) => {
      const snapshot = await get(id)
      return new Promise(resolve => { w.__finishDetail = () => resolve(snapshot) })
    }
    w.runtime.EventsEmit('library:changed')
  })
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishDetail)).toBe('function')
  await drawer.getByRole('button', { name: '关闭', exact: true }).click()
  await page.evaluate(() => {
    const w = window as any
    w.__finishDetail()
    w.runtime.EventsEmit('library:changed')
  })
  await expect(drawer).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__detailReads)).toEqual([1, 1])
})
