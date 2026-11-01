import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

async function setup(page: Page) {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await expect(page.locator('.track-list [data-row-key]')).toHaveCount(7)
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    const copy = (x: any) => JSON.parse(JSON.stringify(x))
    const candidate = { provider: 'qq', matchRef: 'second-track', releaseRef: 'second-album', title: '第二版候选', artist: 'Artist', album: 'Album', confidence: 80,
      badges: ['qq'], action: 'accept', ownership: { state: 'unowned', trackId: 0 }, note: '', localTrackId: 0 }
    const row = (id: number) => ({ id, problemId: 101, batchId: id, batchTag: `source-${id}.csv`, revision: 1, rowIndex: 2, status: 'review',
      inTitle: id === 11 ? 'Song' : 'Song（译名：歌曲）', inArtist: 'Artist', inAlbum: 'Album', rawRow: `["original-${id}"]`,
      inYear: 2020, inDiscNo: 1, inTrackNo: 3, inDurationMs: 200000, candidates: [copy(candidate)], triedVariants: [], lastError: null, lastErrorAt: null, attemptCount: 0 })
    const s = w.__problemActions = { rows: [row(11), row(12)], revision: 1, prepares: [], commits: [], actions: [], plans: {}, confirmation: true, tracks: 0 }
    const active = () => s.rows.filter((r: any) => ['review', 'failed', 'queued', 'matching'].includes(r.status))
    api.GetReviewQueue = async () => ({ items: copy(active()), total: active().length })
    const check = (scope: any) => {
      const members = active().filter((r: any) => r.problemId === scope.problemId).map((r: any) => ({ itemId: r.id, revision: r.revision, inputRevision: r.revision }))
      if (!members.length || JSON.stringify(members) !== JSON.stringify(scope.members)) throw new Error('范围已变化，请重新确认')
    }
    const changed = () => {
      s.revision++; w.__reviewRevision = s.revision
      w.runtime.EventsEmit('review:changed', { revision: s.revision }); w.runtime.EventsEmit('tasks:changed', { revision: s.revision })
    }
    const prepare = (kind: string, scope: any, candidate: any, selected?: number) => {
      check(scope)
      const ids = selected ? [selected] : scope.members.map((m: any) => m.itemId)
      if (ids.some((id: number) => !scope.members.some((m: any) => m.itemId === id))) throw new Error('invalid subset')
      const id = `plan-${s.prepares.length}`
      s.prepares.push({ kind, scope: copy(scope), ids, matchRef: candidate?.matchRef })
      const inputScope = { version: 1, kind: selected ? 'single' : 'problem', problem: { id: scope.problemId, revision: scope.revision },
        members: ids.map((id: number) => ({ itemId: id, batchId: id, rowIndex: 2 })) }
      s.plans[id] = { kind, scope: copy(scope), ids, candidate: copy(candidate ?? {}) }
      return { id, hash: id, seed: { snapshot: {} }, baseline: { inputScope, conflict: false, requiresConfirmation: kind !== 'skip' && s.confirmation, resolution: { groups: [], evidence: [] } } }
    }
    api.GetReviewAdmissionScope = async (scope: any) => { check(scope); return copy(scope) }
    api.PrepareProblemAdmission = async (candidate: any, scope: any) => prepare('accept', scope, candidate)
    api.PrepareProblemSkipExisting = async (candidate: any, scope: any) => prepare('skip', scope, candidate)
    api.PrepareProblemNoMatchNeeded = async (scope: any, id: number) => prepare('original', scope, null, id)
    api.ConfirmReleaseAdditions = async (id: string) => { check(s.plans[id].scope); return { planId: id, hash: id } }
    api.CommitAdmission = async (id: string) => {
      const p = s.plans[id]; check(p.scope)
      s.commits.push(copy(p))
      if (p.kind !== 'skip') s.tracks++
      p.ids.forEach((id: number, i: number) => { const row = s.rows.find((r: any) => r.id === id); row.status = p.kind === 'skip' || i > 0 ? 'skipped' : 'imported' })
      for (const r of active().filter((r: any) => r.problemId === p.scope.problemId)) r.revision++
      changed()
      return { trackId: 99, existing: p.kind === 'skip' }
    }
    api.DiscardReviewProblemMembers = async (scope: any, ids: number[]) => {
      check(scope); s.actions.push({ kind: 'discard', ids: copy(ids) }); ids.forEach(id => { s.rows.find((r: any) => r.id === id).status = 'discarded' }); changed()
    }
    api.SplitReviewProblemItem = async (scope: any, id: number) => {
      check(scope); s.actions.push({ kind: 'split', ids: [id] }); const r = s.rows.find((r: any) => r.id === id); r.problemId = 202; r.revision++; changed(); return 202
    }
    api.RetryReviewProblemMembers = async (scope: any, ids: number[]) => {
      check(scope); if (ids.some(id => s.rows.find((r: any) => r.id === id).status !== 'failed')) throw new Error('非失败成员')
      s.actions.push({ kind: 'retry', ids: copy(ids) }); ids.forEach(id => { const row = s.rows.find((r: any) => r.id === id); row.status = 'queued'; row.revision++ }); changed()
    }
    api.NoneMatchedProblem = async (scope: any) => {
      check(scope); s.actions.push({ kind: 'none', ids: scope.members.map((m: any) => m.itemId) })
      active().forEach((r: any) => { r.candidates = []; r.revision++ }); changed()
    }
    w.__addProblemSource = () => { s.rows.push(row(13)); s.rows.forEach((r: any) => r.revision++); changed() }
  })
}
async function open(page: Page) {
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expect(page.locator('main header')).toContainText('1 个待处理问题')
  await expect(page.getByRole('button', { name: '接受', exact: true })).toBeVisible()
}

test('one problem acceptance creates one track, cancellation preserves draft and exact scope', async ({ page }) => {
  await setup(page); await open(page)
  await page.getByLabel('曲名', { exact: true }).fill('尚未提交草稿')
  await page.getByRole('button', { name: '接受', exact: true }).click()
  await expect(page.getByTestId('admission-input-scope')).toContainText('本次输入范围：2 行')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByLabel('曲名', { exact: true })).toHaveValue('尚未提交草稿')
  await expect(page.getByTestId('problem-scope')).toContainText('上述 2 行')
  expect(await page.evaluate(() => (window as any).__problemActions.tracks)).toBe(0)
  await page.getByRole('button', { name: '接受', exact: true }).click()
  await page.getByRole('button', { name: '确认成员变化并保存', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const s = await page.evaluate(() => (window as any).__problemActions)
  expect(s.commits).toHaveLength(1); expect(s.tracks).toBe(1)
  expect(s.commits[0]).toMatchObject({ ids: [11, 12], candidate: { matchRef: 'second-track' } })
  expect(s.rows.map((r: any) => r.status)).toEqual(['imported', 'skipped'])
})

test('skip ends all displayed sources rather than hiding the candidate and never adds a track', async ({ page }) => {
  await setup(page)
  await page.evaluate(() => (window as any).__problemActions.rows.forEach((r: any) => Object.assign(r.candidates[0], { action: 'skip', ownership: { state: 'owned', trackId: 99 }, localTrackId: 99, badges: ['本地', 'qq'] })))
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await page.getByRole('button', { name: '跳过', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
  const s = await page.evaluate(() => (window as any).__problemActions)
  expect(s.tracks).toBe(0); expect(s.commits).toHaveLength(1)
  expect(s.commits[0]).toMatchObject({ kind: 'skip', ids: [11, 12] })
  expect(s.rows.map((r: any) => r.status)).toEqual(['skipped', 'skipped'])
})

test('a new member rejects the old confirmation and requires reopening the displayed range', async ({ page }) => {
  await setup(page); await open(page)
  await page.getByRole('button', { name: '接受', exact: true }).click()
  await page.evaluate(() => (window as any).__addProblemSource())
  await page.getByRole('button', { name: '确认成员变化并保存', exact: true }).click()
  await expect(page.getByRole('dialog')).toContainText('范围已变化，请重新确认')
  expect(await page.evaluate(() => (window as any).__problemActions.tracks)).toBe(0)
  await expect(page.getByTestId('admission-input-scope')).toContainText('本次输入范围：2 行')
  await page.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByText(/问题范围或证据已变化/)).toBeVisible()
  await page.getByRole('button', { name: '刷新队列', exact: true }).click()
  await expect(page.getByTestId('problem-scope')).toContainText('上述 3 行')
  await page.getByRole('button', { name: '接受', exact: true }).click()
  await expect(page.getByTestId('admission-input-scope')).toContainText('本次输入范围：3 行')
  await page.getByRole('button', { name: '确认成员变化并保存', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
})

test('single-row original admission, split and discard keep other source rows independent', async ({ page }) => {
  await setup(page); await open(page)
  await page.getByLabel('来源行（候选事实及单行操作）').click()
  await page.getByRole('option', { name: /source-12.csv/ }).click()
  await page.getByRole('button', { name: '无需匹配', exact: true }).click()
  await expect(page.getByTestId('admission-input-scope')).toContainText('本次输入范围：1 行')
  await page.getByRole('button', { name: '确认成员变化并保存', exact: true }).click()
  await expect(page.getByTestId('problem-scope')).toContainText('上述 1 行')
  expect(await page.evaluate(() => (window as any).__problemActions.commits[0])).toMatchObject({ kind: 'original', ids: [12] })
  await page.getByRole('button', { name: '移出队列', exact: true }).click()
  await expect(page.getByText('复核队列是空的', { exact: true })).toBeVisible()
})

test('split keeps both problems and none matched retains source rows, retry only lists failed members', async ({ page }) => {
  await setup(page)
  await page.evaluate(() => Object.assign((window as any).__problemActions.rows[1], { status: 'failed', lastError: 'fixture failure', attemptCount: 5 }))
  await open(page)
  await page.getByRole('button', { name: '立即重试', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__problemActions.actions)).toEqual([{ kind: 'retry', ids: [12] }])
  await page.getByRole('button', { name: '拆出此行', exact: true }).click()
  await expect(page.locator('main header')).toContainText('2 个待处理问题')
  await expect(page.getByTestId('problem-scope')).toContainText('上述 1 行')
  await page.getByRole('button', { name: '无一匹配', exact: true }).click()
  await expect(page.getByText('还没有候选结果，可改写条件或换上游再试', { exact: true })).toBeVisible()
  await expect(page.locator('main header')).toContainText('2 个待处理问题')
  expect(await page.evaluate(() => (window as any).__problemActions.rows.map((r: any) => r.problemId))).toEqual([202, 101])
})
