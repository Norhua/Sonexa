import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

const firstTrack = 'f0a33d1a-628a-4a53-bc4e-6af077505956'
const secondTrack = 'f4458acc-aa40-40b0-a0b7-aaa27df24b04'

async function setup(page: Page, scope: 'review' | 'search', twoItems = false) {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(({ scope, firstTrack, secondTrack, twoItems }) => {
    const w = window as any, api = w.go.app.App
    const state = w.__identityActions = { prepares: [] as any[], commits: [] as string[], owned: [firstTrack], done: false, searches: 0, refreshes: 0, failure: false, releaseIDs: { 'release-0': 20, 'release-1': 0 } as Record<string, number> }
    const candidates = [firstTrack, secondTrack].map((matchRef, i) => ({
      provider: 'musicbrainz', matchRef, releaseRef: `release-${i}`, title: 'you', artist: '歌手', album: i ? '歌曲合集' : '特典 CD',
      recordingRef: '2283ceda-78b1-4d20-8f52-bdf73bd53686', isrc: 'JPE301500361', confidence: 95, badges: ['musicbrainz'],
    }))
    const project = (row: any) => {
      const owned = state.owned.includes(row.matchRef), trackId = row.matchRef === firstTrack ? 7 : 90
      if (state.failure) return { ...row, action: 'unavailable', ownership: { state: 'lookup_error', trackId: 0 }, localTrackId: 0, badges: ['musicbrainz'], note: '本地查询失败', coverError: '本地查询失败', releaseId: 0 }
      return { ...row, releaseId: state.releaseIDs[row.releaseRef] ?? 0, coverRevision: 1, coverError: '', note: '', localTrackId: owned ? trackId : 0, ownership: { state: owned ? 'owned' : 'unowned', trackId: owned ? trackId : 0 },
        action: scope === 'review' ? (owned ? 'skip' : 'accept') : (owned ? 'owned' : 'add'), badges: owned ? ['本地', 'musicbrainz'] : ['musicbrainz'] }
    }
    api.SearchUpstream = async () => { state.searches++; return { byProvider: [{ rows: candidates.map(project), capabilities: [] }] } }
    api.RefreshSearchCandidates = async (rows: any[]) => { state.refreshes++; return rows.map(project) }
    api.RefreshReviewCandidates = async (rows: any[]) => { state.refreshes++; return rows.map(project) }
    w.__projectCandidate = project
    const items = w.__identityItems = [{ id: 11, revision: 4, status: 'review', inTitle: '原始 you', inArtist: '歌手', inAlbum: '歌曲合集', batchTag: 'identity.csv', candidates }]
    if (twoItems) items.push({ id: 12, revision: 8, status: 'review', inTitle: '另一个输入', inArtist: '歌手', inAlbum: '歌曲合集', batchTag: 'second.csv', candidates: [candidates[1]] })
    api.GetReviewQueue = async () => ({ items: state.done ? [] : items.map((item: any) => ({ ...item, candidates: item.candidates.map(project) })), total: state.done ? 0 : items.length, pending: state.done ? 0 : items.length, failed: 0 })
    api.ResearchReviewItem = async (id: number) => {
      state.searches++
      const item = items.find((item: any) => item.id === id)
      item.candidates = [{ provider: 'musicbrainz', matchRef: 'new-query-target', releaseRef: 'new-release', title: '新查询结果', album: '新查询专辑', badges: ['musicbrainz'] }]
      item.revision++
      return item.candidates.map(project)
    }
    api.PrepareAdmission = async (row: any, itemId: number) => { state.prepares.push({ action: 'accept', ref: row.matchRef, itemId }); return { id: row.matchRef, hash: 'hash', baseline: { requiresConfirmation: false, conflict: false } } }
    api.PrepareSkipExisting = async (row: any, itemId: number, revision: number) => { state.prepares.push({ action: 'skip', ref: row.matchRef, itemId, revision }); return { id: row.matchRef, hash: 'hash', baseline: { requiresConfirmation: false, conflict: false } } }
    api.CommitAdmission = async (id: string) => {
      state.commits.push(id)
      const existing = state.owned.includes(id)
      if (!existing) state.owned.push(id)
      const itemId = state.prepares.find((plan: any) => plan.ref === id)?.itemId
      const index = items.findIndex((item: any) => item.id === itemId)
      if (index >= 0) items.splice(index, 1)
      state.done = items.length === 0
      return { trackId: id === firstTrack ? 7 : 90, existing }
    }
  }, { scope, firstTrack, secondTrack, twoItems })
  if (scope === 'review') {
    await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  } else {
    await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
    await page.getByRole('combobox').click()
    await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
    await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  }
  await expect(page.getByRole('row').filter({ hasText: '特典 CD' }).getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeEnabled()
  await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
}

/** Only the next response is held; clone at the JSON IPC boundary, not a Vue Proxy. */
async function holdLocalRefresh(page: Page, fail = false) {
  await page.evaluate(fail => {
    const w = window as any, api = w.go.app.App, refresh = api.RefreshReviewCandidates
    api.RefreshReviewCandidates = async (rows: any[]) => {
      api.RefreshReviewCandidates = refresh
      const snapshot = JSON.parse(JSON.stringify(await refresh(rows)))
      return new Promise((resolve, reject) => {
        w.__finishLocal = () => fail ? reject(new Error('过期本地刷新错误')) : resolve(snapshot)
      })
    }
    w.runtime.EventsEmit('library:changed')
  }, fail)
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLocal)).toBe('function')
  await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeDisabled()
}

async function finishLocalRefresh(page: Page) {
  await page.evaluate(async () => {
    (window as any).__finishLocal()
    // Let the released response and Vue rendering settle before negative assertions.
    await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
  })
}

async function emitProgress(page: Page) {
  await page.evaluate(() => (window as any).runtime.EventsEmit('import:progress', {
    batchId: 1, processed: 1, total: 2, stats: { review: 1 }, done: false, paused: false,
  }))
}

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: same ISRC leaves the second album addable and submits its exact track`, async ({ page }) => {
    await setup(page, scope)
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    await expect(first.getByText('本地', { exact: true })).toBeVisible()
    await expect(second.getByText('本地', { exact: true })).toHaveCount(0)
    await expect(first.getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeVisible()
    await second.getByRole('button', { name: scope === 'review' ? '接受' : '加入曲库', exact: true }).click()
    await expect.poll(() => page.evaluate(() => (window as any).__identityActions.commits)).toEqual([secondTrack])
    expect(await page.evaluate(() => (window as any).__identityActions.prepares)).toEqual([{ action: 'accept', ref: secondTrack, itemId: scope === 'review' ? 11 : 0 }])
    if (scope === 'review') await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
    else await expect(second.getByRole('button', { name: '查看库中曲目：you', exact: true })).toBeVisible()
  })
}

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: library changes refresh actions and own covers without re-searching`, async ({ page }) => {
    await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"><rect width="20" height="20" fill="blue"/></svg>' }))
    await setup(page, scope)
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    const input = scope === 'review' ? page.getByLabel('曲名', { exact: true }) : page.getByPlaceholder('在 MusicBrainz 中搜索')
    await input.fill('保留未提交草稿')
    await expect(first.locator('img')).toHaveAttribute('src', /releaseId=20.*ref=release-0/)
    await expect(second.locator('img')).toHaveAttribute('src', /ref=release-1/)
    await expect(second.locator('img')).not.toHaveAttribute('src', /releaseId=20/)
    for (const owned of [[], [secondTrack], [firstTrack]]) {
      await page.evaluate(owned => { const w = window as any; w.__identityActions.owned = owned; w.__identityActions.releaseIDs = owned.length ? { 'release-0': 0, 'release-1': 21 } : {}; w.runtime.EventsEmit('library:changed') }, owned)
      const firstOwned = owned.includes(firstTrack), secondOwned = owned.includes(secondTrack)
      await expect(first.getByRole('button', { name: scope === 'review' ? (firstOwned ? '跳过' : '接受') : (firstOwned ? '查看库中曲目：you' : '加入曲库'), exact: true })).toBeEnabled()
      await expect(second.getByRole('button', { name: scope === 'review' ? (secondOwned ? '跳过' : '接受') : (secondOwned ? '查看库中曲目：you' : '加入曲库'), exact: true })).toBeEnabled()
      await expect(input).toHaveValue('保留未提交草稿')
      await expect(first.locator('img')).not.toHaveAttribute('src', /releaseId=21/)
    }
    await page.evaluate(() => { const w = window as any; w.__identityActions.failure = true; w.runtime.EventsEmit('library:changed') })
    await expect(page.getByRole('button', { name: '不可操作', exact: true })).toHaveCount(2)
    await expect(first.getByRole('button', { name: '不可操作', exact: true })).toBeDisabled()
    await expect(first.locator('img')).toHaveCount(0)
    await page.evaluate(() => { (window as any).__identityActions.failure = false })
    await page.getByRole('button', { name: '刷新本地状态', exact: true }).click()
    await expect(first.getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeEnabled()
    const state = await page.evaluate(() => (window as any).__identityActions)
    expect(state.searches).toBe(scope === 'search' ? 1 : 0)
    expect(state.commits).toEqual([])
  })
}

for (const scope of ['review', 'search'] as const) {
  test(`${scope}: rejected local refresh preserves facts but disables stale ownership until retry`, async ({ page }) => {
    await setup(page, scope)
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    await page.evaluate(scope => {
      const w = window as any, api = w.go.app.App
      const name = scope === 'review' ? 'RefreshReviewCandidates' : 'RefreshSearchCandidates'
      const refresh = api[name]
      api[name] = async () => { api[name] = refresh; throw new Error('整次本地查询失败') }
      w.runtime.EventsEmit('library:changed')
    }, scope)
    await expect(page.getByText('Error: 整次本地查询失败', { exact: true })).toBeVisible()
    for (const row of [first, second]) {
      await expect(row.getByRole('button', { name: '不可操作', exact: true })).toBeDisabled()
      await expect(row.getByText('本地', { exact: true })).toHaveCount(0)
      await expect(row.locator('img')).toHaveCount(0)
    }
    await page.evaluate(secondTrack => { (window as any).__identityActions.owned = [secondTrack] }, secondTrack)
    await page.getByRole('button', { name: '刷新本地状态', exact: true }).click()
    await expect(page.getByText('Error: 整次本地查询失败', { exact: true })).toHaveCount(0)
    await expect(first.getByRole('button', { name: scope === 'review' ? '接受' : '加入曲库', exact: true })).toBeEnabled()
    await expect(second.getByRole('button', { name: scope === 'review' ? '跳过' : '查看库中曲目：you', exact: true })).toBeEnabled()
    const state = await page.evaluate(() => (window as any).__identityActions)
    expect(state.searches).toBe(scope === 'review' ? 0 : 1)
    expect(state.commits).toEqual([])
  })
}

test('review: late local refresh cannot overwrite a newer persisted search', async ({ page }) => {
  await setup(page, 'review')
  await holdLocalRefresh(page)
  await page.getByLabel('曲名', { exact: true }).fill('新查询草稿')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect(page.getByText('新查询结果', { exact: true })).toBeVisible()
  await finishLocalRefresh(page)
  await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
  await expect(page.getByText('新查询结果', { exact: true })).toBeVisible()
  await expect(page.getByText('特典 CD', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('新查询草稿')
  await page.getByRole('button', { name: '接受', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const state = await page.evaluate(() => (window as any).__identityActions)
  expect(state.prepares).toEqual([{ action: 'accept', ref: 'new-query-target', itemId: 11 }])
  expect(state.commits).toEqual(['new-query-target'])
  expect(state.searches).toBe(1)
})

for (const fail of [false, true]) {
  test(`review: late local response cannot affect another item (failure: ${fail})`, async ({ page }) => {
    await setup(page, 'review', true)
    await holdLocalRefresh(page, fail)
    await page.evaluate(secondTrack => { (window as any).__identityActions.owned = [secondTrack] }, secondTrack)
    await page.getByRole('button', { name: /下一条/ }).click()
    await expect(page.getByText('另一个输入', { exact: true })).toBeVisible()
    await page.getByLabel('曲名', { exact: true }).fill('第二项草稿')
    await finishLocalRefresh(page)
    await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
    await expect(page.getByText('特典 CD', { exact: true })).toHaveCount(0)
    await expect(page.getByText('过期本地刷新错误', { exact: false })).toHaveCount(0)
    await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('第二项草稿')
    await page.getByRole('button', { name: '跳过', exact: true }).click()
    await expect(page.getByText('原始 you', { exact: true })).toBeVisible()
    await expect(page.getByText('另一个输入', { exact: true })).toHaveCount(0)
    const state = await page.evaluate(() => (window as any).__identityActions)
    expect(state.prepares).toEqual([{ action: 'skip', ref: secondTrack, itemId: 12, revision: 8 }])
    expect(state.commits).toEqual([secondTrack])
    expect(state.searches).toBe(0)
  })

  test(`review: unmounted local response is ignored after reopening (failure: ${fail})`, async ({ page }) => {
    await setup(page, 'review')
    await holdLocalRefresh(page, fail)
    await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
    await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toHaveCount(0)
    const reads = await page.evaluate(async secondTrack => {
      const w = window as any, reads = w.__identityActions.refreshes
      w.__identityActions.owned = [secondTrack]
      w.runtime.EventsEmit('library:changed')
      await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
      return { before: reads, after: w.__identityActions.refreshes }
    }, secondTrack)
    expect(reads.after).toBe(reads.before)
    await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
    await expect(page.getByRole('row').filter({ hasText: '歌曲合集' }).getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
    await finishLocalRefresh(page)
    await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
    await expect(page.getByText('过期本地刷新错误', { exact: false })).toHaveCount(0)
    await expect(page.getByRole('row').filter({ hasText: '特典 CD' }).getByRole('button', { name: '接受', exact: true })).toBeEnabled()
    await expect(page.getByRole('row').filter({ hasText: '歌曲合集' }).getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
    expect(await page.evaluate(() => (window as any).__identityActions.searches)).toBe(0)
  })
}

for (const localFirst of [false, true]) {
  test(`review: queue and library responses retain current identity and cover (local first: ${localFirst})`, async ({ page }) => {
    await page.route('**/cover?*', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="20" height="20"/>' }))
    await setup(page, 'review')
    await page.getByLabel('曲名', { exact: true }).fill('交错刷新草稿')
    await page.evaluate(() => {
      const w = window as any, api = w.go.app.App, get = api.GetReviewQueue
      api.GetReviewQueue = async () => {
        api.GetReviewQueue = get
        const snapshot = JSON.parse(JSON.stringify(await get()))
        return new Promise(resolve => { w.__finishQueue = () => resolve(snapshot) })
      }
    })
    await emitProgress(page)
    await expect.poll(() => page.evaluate(() => typeof (window as any).__finishQueue)).toBe('function')
    await page.evaluate(secondTrack => {
      const state = (window as any).__identityActions
      state.owned = [secondTrack]
      state.releaseIDs = { 'release-1': 21 }
    }, secondTrack)
    await holdLocalRefresh(page)
    if (localFirst) {
      await finishLocalRefresh(page)
      await expect(page.getByRole('row').filter({ hasText: '歌曲合集' }).getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
      await page.evaluate(() => (window as any).__finishQueue())
    } else {
      await page.evaluate(() => (window as any).__finishQueue())
      await finishLocalRefresh(page)
    }
    await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
    const first = page.getByRole('row').filter({ hasText: '特典 CD' })
    const second = page.getByRole('row').filter({ hasText: '歌曲合集' })
    await expect(first.getByRole('button', { name: '接受', exact: true })).toBeEnabled()
    await expect(first.getByText('本地', { exact: true })).toHaveCount(0)
    await expect(second.getByRole('button', { name: '跳过', exact: true })).toBeEnabled()
    await expect(second.getByText('本地', { exact: true })).toBeVisible()
    await expect(first.locator('img')).toHaveAttribute('src', /ref=release-0/)
    await expect(first.locator('img')).not.toHaveAttribute('src', /releaseId=/)
    await expect(second.locator('img')).toHaveAttribute('src', /releaseId=21.*ref=release-1/)
    await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('交错刷新草稿')
    const state = await page.evaluate(() => (window as any).__identityActions)
    expect(state.searches).toBe(0)
    expect(state.commits).toEqual([])
  })
}

test('review: library change during research refreshes the new target and revision', async ({ page }) => {
  await setup(page, 'review')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, research = api.ResearchReviewItem
    api.ResearchReviewItem = async (id: number) => {
      const snapshot = JSON.parse(JSON.stringify(await research(id)))
      return new Promise(resolve => { w.__finishResearch = () => resolve(snapshot) })
    }
  })
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishResearch)).toBe('function')
  await page.evaluate(() => {
    const w = window as any
    w.__identityActions.owned = ['new-query-target']
    w.runtime.EventsEmit('library:changed')
  })
  await expect(page.getByRole('button', { name: '跳过', exact: true })).toHaveCount(0)
  await page.evaluate(() => (window as any).__finishResearch())
  await expect(page.getByText('新查询结果', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '刷新本地状态', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: '跳过', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const state = await page.evaluate(() => (window as any).__identityActions)
  expect(state.prepares).toEqual([{ action: 'skip', ref: 'new-query-target', itemId: 11, revision: 5 }])
  expect(state.commits).toEqual(['new-query-target'])
  expect(state.searches).toBe(1)
})

test('review: skip finishes the input instead of hiding a candidate', async ({ page }) => {
  await setup(page, 'review')
  await page.getByRole('row').filter({ hasText: '特典 CD' }).getByRole('button', { name: '跳过', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const state = await page.evaluate(() => (window as any).__identityActions)
  expect(state.prepares).toEqual([{ action: 'skip', ref: firstTrack, itemId: 11, revision: 4 }])
  expect(state.owned).toEqual([firstTrack])
  expect(state.commits).toEqual([firstTrack])
})
