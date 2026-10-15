import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

// Go 的 error-only 方法经 Wails 成功返回 null；状态必须与页面副本隔离，
// 否则直接修改桩件对象也会刷新 Vue，掩盖动作完成后没有重载队列的问题。
test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(async () => {
    const w = window as any, api = w.go.app.App
    const queue = await api.GetReviewQueue()
    queue.items[0].candidates = [{
      provider: 'qq', matchRef: 'candidate-11', title: '复核候选', artist: '候选艺术家',
      album: '候选专辑', badges: ['qq'], action: '接受', confidence: 90, localTrackId: 0,
    }]
    const state = w.__review = {
      items: queue.items, queueReads: 0, taskReads: 0, actions: [], commits: [], prepares: [],
      failure: '', requiresConfirmation: true,
    }
    const pending = () => state.items.filter((item: any) => ['review', 'failed'].includes(item.status))
    api.GetReviewQueue = async () => {
      state.queueReads++
      const items = pending()
      return structuredClone({
        items, total: items.length,
        pending: items.filter((item: any) => item.status === 'review').length,
        failed: items.filter((item: any) => item.status === 'failed').length,
      })
    }
    api.GetTaskSummary = async () => {
      state.taskReads++
      return { activeBatch: 0, filename: '', processed: 0, total: 0, stats: null, running: false, reviewPending: pending().length }
    }
    const action = (name: string, status?: string) => async (itemId: number, targetTrackId?: number) => {
      state.actions.push({ name, itemId, targetTrackId })
      if (state.failure) throw new Error(state.failure)
      const item = state.items.find((item: any) => item.id === itemId)
      if (status) item.status = status
      else item.candidates = []
      return null
    }
    api.DiscardReviewItem = action('discard', 'discarded')
    api.NoneMatched = action('none')
    api.MergeCandidate = action('merge', 'merged')
    api.RetryReviewItem = action('retry', 'queued')
    const prepare = (itemId: number) => {
      state.prepares.push(itemId)
      return {
        id: `admit-${itemId}`, hash: 'hash', seed: { snapshot: {} },
        baseline: { conflict: false, requiresConfirmation: state.requiresConfirmation, resolution: { groups: [], evidence: [] } },
      }
    }
    api.PrepareAdmission = async (_candidate: any, itemId: number) => prepare(itemId)
    api.PrepareNoMatchNeeded = async (itemId: number) => prepare(itemId)
    api.ConfirmReleaseAdditions = async (id: string, hash: string) => ({ planId: id, hash, credential: 'confirmed' })
    api.CommitAdmission = async (id: string) => {
      state.commits.push(id)
      state.items.find((item: any) => id === `admit-${item.id}`).status = 'imported'
      return { trackId: 90 }
    }
  })
})

async function expectCounts(page: Page, total: number, pending = total, failed = 0) {
  await expect(page.locator('main header')).toContainText(`${total} 条待处置 · 待选 ${pending} · 失败 ${failed}`)
  const navigation = page.locator('nav').getByRole('button', { name: /复核队列/ })
  await expect(navigation).toHaveText(total ? new RegExp(`复核队列\\s*${total}$`) : '复核队列')
}

async function openReview(page: Page, pending = 2, failed = 0) {
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expectCounts(page, pending + failed, pending, failed)
  await expect(page.getByText('ツバサ（羽翼）', { exact: true })).toBeVisible()
}

async function expectSecondItem(page: Page) {
  await expectCounts(page, 1)
  await expect(page.getByText('you', { exact: true })).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('you')
  await expect(page.getByText('1 / 1', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /上一条/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /下一条/ })).toBeDisabled()
}

test('discard returning null immediately advances the queue, updates counts and reaches the empty state', async ({ page }) => {
  await openReview(page)
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expectSecondItem(page)
  await expect(page.getByText('复核候选', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expectCounts(page, 0)
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: '移出队列', exact: true })).toHaveCount(0)
  const state = await page.evaluate(() => (window as any).__review)
  expect(state.actions).toMatchObject([{ name: 'discard', itemId: 11 }, { name: 'discard', itemId: 12 }])
  expect(state.queueReads).toBe(3)
  expect(state.taskReads).toBe(3)
  expect(state.commits).toEqual([])
})

test('discarding the last position selects the remaining previous item', async ({ page }) => {
  await openReview(page)
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByText('you', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expectCounts(page, 1)
  await expect(page.getByText('ツバサ（羽翼）', { exact: true })).toBeVisible()
  await expect(page.getByText('1 / 1', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: /上一条/ })).toBeDisabled()
  await expect(page.getByRole('button', { name: /下一条/ })).toBeDisabled()
  expect(await page.evaluate(() => (window as any).__review.actions)).toMatchObject([{ name: 'discard', itemId: 12 }])
})

test('none matched returning null clears candidates but keeps the item and counts', async ({ page }) => {
  await openReview(page)
  await expect(page.getByText('复核候选', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '无一匹配', exact: true }).click()
  await expect(page.getByText('复核候选', { exact: true })).toHaveCount(0)
  await expect(page.getByText('还没有候选结果，可改写条件或换上游再试', { exact: true })).toBeVisible()
  await expect(page.getByText('ツバサ（羽翼）', { exact: true })).toBeVisible()
  await expectCounts(page, 2)
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(2)
})

test('merge returning null advances the queue without admitting another track', async ({ page }) => {
  await page.evaluate(() => {
    Object.assign((window as any).__review.items[0].candidates[0], { localTrackId: 7, action: '合并', badges: ['本地', 'qq'] })
  })
  await openReview(page)
  await page.getByRole('button', { name: '合并', exact: true }).click()
  await expectSecondItem(page)
  const state = await page.evaluate(() => (window as any).__review)
  expect(state.actions).toEqual([{ name: 'merge', itemId: 11, targetTrackId: 7 }])
  expect(state.commits).toEqual([])
  expect(state.prepares).toEqual([])
})

test('retry returning null refreshes the failed count after requeuing the item', async ({ page }) => {
  await page.evaluate(() => {
    Object.assign((window as any).__review.items[0], { status: 'failed', attemptCount: 3, lastError: '上游暂不可用' })
  })
  await openReview(page, 1, 1)
  await expect(page.getByText('上游暂不可用', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: '立即重试', exact: true }).click()
  await expectSecondItem(page)
  await expect(page.getByRole('button', { name: '立即重试', exact: true })).toHaveCount(0)
  const state = await page.evaluate(() => (window as any).__review)
  expect(state.items[0].status).toBe('queued')
  expect(state.actions).toMatchObject([{ name: 'retry', itemId: 11 }])
})

test('failed discard keeps the item and search draft, shows an error and can be retried', async ({ page }) => {
  await openReview(page)
  await page.getByLabel('曲名', { exact: true }).fill('未提交搜索草稿')
  await page.evaluate(() => { (window as any).__review.failure = '移出失败，请重试' })
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expect(page.getByText('Error: 移出失败，请重试', { exact: true })).toBeVisible()
  await expectCounts(page, 2)
  await expect(page.getByText('ツバサ（羽翼）', { exact: true })).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('未提交搜索草稿')
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(1)
  await page.evaluate(() => { (window as any).__review.failure = '' })
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expectSecondItem(page)
  await expect(page.getByText('Error: 移出失败，请重试', { exact: true })).toHaveCount(0)
})

for (const label of ['接受', '无需匹配']) {
  test(`${label}: cancelled admission preserves the draft and candidates; confirmed admission refreshes`, async ({ page }) => {
    await openReview(page)
    await page.getByLabel('曲名', { exact: true }).fill('未提交搜索草稿')
    await page.getByRole('button', { name: label, exact: true }).click()
    const confirm = page.getByRole('dialog', { name: '确认发行成员变化', exact: true })
    await expect(confirm).toBeVisible()
    await confirm.getByRole('button', { name: '取消', exact: true }).click()
    await expect(confirm).toHaveCount(0)
    await expectCounts(page, 2)
    await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('未提交搜索草稿')
    await expect(page.getByText('复核候选', { exact: true })).toBeVisible()
    const cancelled = await page.evaluate(() => (window as any).__review)
    expect(cancelled.queueReads).toBe(1)
    expect(cancelled.taskReads).toBe(1)
    expect(cancelled.commits).toEqual([])
    expect(cancelled.prepares).toEqual([11])
    await page.getByRole('button', { name: label, exact: true }).click()
    await confirm.getByRole('button', { name: '确认成员变化并保存', exact: true }).click()
    await expect(confirm).toHaveCount(0)
    await expectSecondItem(page)
    expect(await page.evaluate(() => (window as any).__review.commits)).toEqual(['admit-11'])
  })

  test(`${label}: admission without extra confirmation also refreshes immediately`, async ({ page }) => {
    await page.evaluate(() => { (window as any).__review.requiresConfirmation = false })
    await openReview(page)
    await page.getByRole('button', { name: label, exact: true }).click()
    await expectSecondItem(page)
    await expect(page.getByRole('dialog')).toHaveCount(0)
    expect(await page.evaluate(() => (window as any).__review.commits)).toEqual(['admit-11'])
  })
}

/** 真实导入逐条推送进度；包括失败数和完成事件，不借由重新进入页面刷新。 */
async function emitProgress(page: Page) {
  await page.evaluate(() => {
    const w = window as any, items = w.__review.items.filter((item: any) => ['review', 'failed'].includes(item.status))
    w.runtime.EventsEmit('import:progress', {
      batchId: 1, processed: 3, total: 10, done: false, paused: false, line: '',
      stats: { review: items.filter((item: any) => item.status === 'review').length, failed: items.filter((item: any) => item.status === 'failed').length },
    })
  })
}

test('import adds an item to an already open empty review queue', async ({ page }) => {
  await page.evaluate(() => { (window as any).__review.items = [] })
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  await page.evaluate(() => {
    (window as any).__review.items.push({ id: 13, batchId: 1, status: 'review', inTitle: '新到复核条目', inArtist: '歌手', inAlbum: '专辑', candidates: [], batchTag: '导入中.csv' })
  })
  await emitProgress(page)
  await expectCounts(page, 1)
  await expect(page.getByText('新到复核条目', { exact: true })).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('新到复核条目')
  await expect(page.getByText('复核队列是空的', { exact: true })).toHaveCount(0)
})

test('background refresh preserves item identity, search draft, provider and expanded candidates', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.__review.items[0].candidates = Array.from({ length: 12 }, (_, i) => ({ ...w.__review.items[0].candidates[0], title: `候选 ${i + 1}`, matchRef: `candidate-${i}` }))
  })
  await openReview(page)
  await page.getByLabel('曲名', { exact: true }).fill('尚未提交的曲名')
  await page.getByLabel('艺术家', { exact: true }).fill('尚未提交的艺术家')
  await page.getByLabel('专辑', { exact: true }).fill('尚未提交的专辑')
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'iTunes', exact: true }).click()
  await page.getByRole('button', { name: '显示全部', exact: true }).click()
  await expect(page.getByText('候选 12', { exact: true })).toBeVisible()
  await page.evaluate(() => {
    const state = (window as any).__review
    state.items.unshift({ ...structuredClone(state.items[1]), id: 10, inTitle: '前方新增条目' })
    state.items.push({ ...structuredClone(state.items[1]), id: 13, inTitle: '后方新增条目' })
  })
  await emitProgress(page)
  await expectCounts(page, 4)
  await expect(page.getByText('2 / 4', { exact: true })).toBeVisible()
  await expect(page.getByText('ツバサ（羽翼）', { exact: true })).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('尚未提交的曲名')
  await expect(page.getByLabel('艺术家', { exact: true })).toHaveValue('尚未提交的艺术家')
  await expect(page.getByLabel('专辑', { exact: true })).toHaveValue('尚未提交的专辑')
  await expect(page.getByRole('combobox').first()).toContainText('iTunes')
  await expect(page.getByText('候选 12', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__calls.filter((name: string) => name === 'ResearchReviewItem'))).toEqual([])
  await page.getByRole('button', { name: /下一条/ }).click()
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByText('后方新增条目', { exact: true })).toBeVisible()
})

test('unchanged queue size still refreshes failure status and candidates', async ({ page }) => {
  await openReview(page)
  await page.getByLabel('曲名', { exact: true }).fill('保留输入')
  await page.evaluate(() => {
    const item = (window as any).__review.items[0]
    Object.assign(item, { status: 'failed', attemptCount: 3, lastError: '新失败原因', candidates: [] })
  })
  await emitProgress(page)
  await expectCounts(page, 2, 1, 1)
  await expect(page.getByText('新失败原因', { exact: false })).toBeVisible()
  await expect(page.getByText('复核候选', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('保留输入')
})

for (const finishSearchFirst of [false, true]) {
  test(`queue refresh and interactive search retain the latest candidates (search first: ${finishSearchFirst})`, async ({ page }) => {
    await openReview(page)
    await page.evaluate(() => {
      const w = window as any, api = w.go.app.App, get = api.GetReviewQueue
      api.ResearchReviewItem = async (id: number) => new Promise(resolve => {
        w.__finishResearch = () => {
          const item = w.__review.items.find((item: any) => item.id === id)
          item.candidates = [{ ...item.candidates[0], title: '最新手动搜索结果' }]
          resolve(structuredClone(item.candidates))
        }
      })
      api.GetReviewQueue = async () => {
        const snapshot = await get()
        return new Promise(resolve => { w.__finishQueue = () => resolve(snapshot) })
      }
    })
    await page.getByLabel('曲名', { exact: true }).fill('正在搜索的草稿')
    await page.getByRole('button', { name: '重新搜索', exact: true }).click()
    await expect(page.getByRole('button', { name: '搜索中…', exact: true })).toBeDisabled()
    await emitProgress(page)
    await expect.poll(() => page.evaluate(() => typeof (window as any).__finishQueue)).toBe('function')
    if (finishSearchFirst) {
      await page.evaluate(() => (window as any).__finishResearch())
      await expect(page.getByText('最新手动搜索结果', { exact: true })).toBeVisible()
      await page.evaluate(() => (window as any).__finishQueue())
    } else {
      await page.evaluate(() => (window as any).__finishQueue())
      await expect.poll(() => page.evaluate(() => (window as any).__review.taskReads)).toBe(2)
      await expect(page.getByRole('button', { name: '搜索中…', exact: true })).toBeDisabled()
      await page.evaluate(() => (window as any).__finishResearch())
    }
    await expect.poll(() => page.evaluate(() => (window as any).__review.taskReads)).toBe(2)
    await expect(page.getByText('最新手动搜索结果', { exact: true })).toBeVisible()
    await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('正在搜索的草稿')
    await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeEnabled()
  })
}

test('progress bursts are coalesced and an old queue reply cannot undo discard', async ({ page }) => {
  await openReview(page)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, get = api.GetReviewQueue
    let hold = true
    api.GetReviewQueue = async () => {
      const snapshot = await get()
      if (!hold) return snapshot
      hold = false
      return new Promise(resolve => { w.__finishQueue = () => resolve(snapshot) })
    }
  })
  await emitProgress(page)
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishQueue)).toBe('function')
  for (let i = 0; i < 5; i++) await emitProgress(page)
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(2)
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__review.items[0].status)).toBe('discarded')
  await page.evaluate(() => (window as any).__finishQueue())
  await expectSecondItem(page)
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(3)
})

test('failed live refresh keeps current content and retries on the next event; leaving unsubscribes', async ({ page }) => {
  await openReview(page)
  await page.getByLabel('曲名', { exact: true }).fill('保留草稿')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, get = api.GetReviewQueue
    let fail = true
    api.GetReviewQueue = async () => {
      if (fail) { fail = false; throw new Error('读取复核队列失败') }
      return get()
    }
  })
  await emitProgress(page)
  await expect(page.getByText('Error: 读取复核队列失败', { exact: true })).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('保留草稿')
  await emitProgress(page)
  await expect.poll(() => page.evaluate(() => (window as any).__review.queueReads)).toBe(2)
  await expect(page.getByText('Error: 读取复核队列失败', { exact: true })).toHaveCount(0)
  await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await emitProgress(page)
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(2)
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expectCounts(page, 2)
  expect(await page.evaluate(() => (window as any).__review.queueReads)).toBe(3)
})

test('same-item refresh keeps manual-search history and does not trigger extra searches', async ({ page }) => {
  await openReview(page)
  await page.getByLabel('曲名', { exact: true }).fill('手动搜索条件')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect(page.getByText('手动搜索条件', { exact: true })).toBeVisible()
  await emitProgress(page)
  await expect.poll(() => page.evaluate(() => (window as any).__review.taskReads)).toBe(2)
  expect(await page.evaluate(() => (window as any).__calls.filter((name: string) => name === 'ResearchReviewItem').length)).toBe(1)
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'QQ 音乐', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__lastResearch?.providerID)).toBe('qq')
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('手动搜索条件')
})

test('late queue and search replies cannot restore a previous selection or an unmounted review page', async ({ page }) => {
  await openReview(page)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, get = api.GetReviewQueue
    api.ResearchReviewItem = async () => new Promise(resolve => {
      w.__finishResearch = () => resolve([{ title: '已离开条目的旧搜索结果', badges: [], action: '接受' }])
    })
    api.GetReviewQueue = async () => {
      const snapshot = await get()
      return new Promise(resolve => { w.__finishQueue = () => resolve(snapshot) })
    }
  })
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await emitProgress(page)
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishQueue)).toBe('function')
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByText('you', { exact: true })).toBeVisible()
  await page.evaluate(() => { const w = window as any; w.__finishQueue(); w.__finishResearch() })
  await expect.poll(() => page.evaluate(() => (window as any).__review.taskReads)).toBe(2)
  await expect(page.getByText('you', { exact: true })).toBeVisible()
  await expect(page.getByText('已离开条目的旧搜索结果', { exact: true })).toHaveCount(0)
  await page.evaluate(() => { delete (window as any).__finishQueue })
  await emitProgress(page)
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishQueue)).toBe('function')
  await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => (window as any).__finishQueue())
  await expect(page.locator('main')).not.toContainText('you')
  // 第二次进度事件仍由全局 tasks 汇总一次；卸载队列的旧响应不得追加读取。
  expect(await page.evaluate(() => (window as any).__review.taskReads)).toBe(3)
})
