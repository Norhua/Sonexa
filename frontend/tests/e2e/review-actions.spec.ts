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
