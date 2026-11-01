import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, copy = (x: any) => JSON.parse(JSON.stringify(x))
    const row = (id: number) => ({ id: id + 100, batchId: id, batchTag: `batch-${id}.csv`, revision: 1, rowIndex: 0, rawRow: '["original"]', status: 'review', inTitle: `Song ${id}`, inArtist: 'Artist', inAlbum: 'Album', candidates: [], triedVariants: [] })
    const s = w.__leaseState = { revision: 1, acquired: [], released: [], writes: [], searches: [], lease: {}, items: [1, 2].map(id => ({ id, status: 'pending', query: null, sources: [row(id)], scope: { version: 1, problemId: id, revision: 1, queryRevision: 1, candidateRevision: 1, members: [{ itemId: id + 100, revision: 1, inputRevision: 1 }] } })) }
    const changed = () => { s.revision++; w.runtime.EventsEmit('review:changed', { revision: s.revision }) }
    const problem = (id: number) => s.items.find((p: any) => p.id === id)
    const check = (scope: any) => {
      const p = problem(scope.problemId)
      if (!p || scope.revision !== p.scope.revision || scope.leaseToken !== s.lease[p.id]) throw new Error('stale scope/lease')
      return p
    }
    api.GetReviewProblemQueue = async () => copy({ revision: s.revision, items: s.items, problems: { reviewProblems: s.items.length, pendingProblems: s.items.length, failedProblems: 0, sourceRows: s.items.length } })
    api.GetReviewProblemScope = async (id: number) => copy(problem(id).scope)
    api.GetTaskSummary = async () => ({ activeBatch: 0, filename: '', total: 0, processed: 0, running: false, stats: null, problems: { reviewProblems: s.items.length, sourceRows: s.items.length, pendingProblems: s.items.length, failedProblems: 0 } })
    api.AcquireReviewProblemLease = async (scope: any) => {
      const p = problem(scope.problemId)
      if (scope.revision !== p.scope.revision) throw new Error('old acquire')
      const token = `token-${p.id}-${s.acquired.length}`
      s.acquired.push(p.id); s.lease[p.id] = token
      p.scope.revision++; p.scope.queryRevision++; changed()
      return { scope: { ...copy(p.scope), leaseToken: token }, lease: { problemId: p.id, token, expiresAt: Date.now() + 90000 } }
    }
    api.ReleaseReviewProblemLease = async (lease: any) => { s.released.push(lease.problemId); if (s.lease[lease.problemId] === lease.token) delete s.lease[lease.problemId] }
    api.UpdateReviewProblemDraft = async (scope: any, query: any) => {
      const p = check(scope); s.writes.push(copy(query)); p.query = copy(query); p.scope.revision++; p.scope.queryRevision++; changed()
      return { ...copy(p.scope), leaseToken: scope.leaseToken }
    }
    api.ResearchReviewProblem = async (scope: any, query: any) => {
      const p = check(scope); p.scope.revision++; p.scope.queryRevision++; s.searches.push(copy(query)); changed()
      return new Promise(resolve => { w.__finishSearch = () => resolve([{ provider: 'qq', matchRef: 'late-track', title: '迟到候选', action: 'accept', ownership: { state: 'unowned' }, badges: ['qq'] }]) })
    }
  })
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.acquired)).toEqual([1])
})

test('draft revisions invalidate an in-flight search while background snapshots preserve text', async ({ page }) => {
  await page.getByLabel('曲名', { exact: true }).fill('first draft')
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.searches.length)).toBe(1)
  await page.getByLabel('曲名', { exact: true }).fill('new draft during search')
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.writes.at(-1)?.title)).toBe('new draft during search')
  await page.evaluate(() => (window as any).__finishSearch())
  await expect(page.getByText('迟到候选', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('new draft during search')
  await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeEnabled()
  await expect(page.getByText(/问题范围或证据已变化/)).toHaveCount(0)
})

test('switching and unloading release leases and reject the old search response', async ({ page }) => {
  await page.getByRole('button', { name: '重新搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.searches.length)).toBe(1)
  await page.getByRole('button', { name: /下一条/ }).click()
  await expect(page.getByTestId('problem-title')).toHaveText('Song 2')
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.released)).toEqual([1])
  await page.evaluate(() => (window as any).__finishSearch())
  await expect(page.getByText('迟到候选', { exact: true })).toHaveCount(0)
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('Song 2')
  await page.locator('nav').getByRole('button', { name: '全部曲目', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.released)).toEqual([1, 2])
})

test('renewal failure requires reopening but retains the draft', async ({ page }) => {
  await page.getByLabel('曲名', { exact: true }).fill('retained draft')
  await expect.poll(() => page.evaluate(() => (window as any).__leaseState.writes.length)).toBe(1)
  await page.clock.install()
  // The timer existed before clock installation; reopen installs a managed timer.
  await page.getByRole('button', { name: '刷新队列', exact: true }).click()
  await page.evaluate(() => { (window as any).go.app.App.RenewReviewProblemLease = async () => { throw new Error('expired') } })
  await page.clock.fastForward(31000)
  await expect(page.getByText(/交互保护已失效/)).toBeVisible()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('retained draft')
  await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: '刷新队列', exact: true }).click()
  await expect(page.getByText(/交互保护已失效/)).toHaveCount(0)
  await expect(page.getByRole('button', { name: '重新搜索', exact: true })).toBeEnabled()
})
