import { test, expect, type Page } from './fixtures'
import { fileURLToPath } from 'node:url'

// 修改 4：组内已有发行能提供这首歌时的确认弹窗、输入确认与判组三种动作。
// 闸门在服务端；这里的桩件按同样的规则校验确认词，验证的是前端流程。
test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    const state = w.__c4 = { mode: 'provided', plans: 0, commits: [] as any[], typed: [] as string[], used: [] as number[], refreshed: 0, choices: [] as any[], editSaves: [] as any[], refreshFails: false, refreshTo: 'provided' }
    const releaseA = { releaseId: 21, identity: { provider: 'musicbrainz', ref: 'release-a' }, title: '已有发行 A', credit: '专辑艺术家', trackRefs: 4, trackTotal: 12 }
    const group = (id: number) => ({ id, title: `专辑组 ${id}`, credit: '专辑艺术家', members: [{ id: id + 20, title: `已有发行 ${id}`, identity: { provider: 'musicbrainz', ref: `release-${id}` }, trackIds: [id] }] })
    const base = (extra: any = {}, seed: any = {}) => ({
      id: `plan-${++state.plans}`, hash: 'hash',
      seed: { snapshot: { release: { identity: { provider: 'musicbrainz', ref: 'release-b' }, title: { state: 'value', value: '发行 B' }, credit: { state: 'value', value: { text: '专辑艺术家' } } } }, moveReleaseId: 0, mergeTargetId: 0, joinGroupId: 0, keepSeparate: false, isEdit: false, ...seed },
      baseline: { coverage: 'not_applicable', needsTypedConfirm: false, requiresConfirmation: false, conflict: false, existingReleaseId: 0, resolution: { groups: [group(1)], evidence: [{ groupId: 1, kind: 'native', native: { provider: 'musicbrainz', ref: 'rg-1' } }] }, ...extra },
    })
    w.__makePlan = (mode = state.mode, seed: any = {}) => {
      if (mode === 'provided') return base({ coverage: 'provided', needsTypedConfirm: true, requiresConfirmation: true, providers: [
        { ...releaseA, discNumber: 1, trackNumber: 5, trackTitle: '共同歌曲', durationMs: 201000, trackRef: 'a-5', recordingRef: 'rec-x', evidence: 'recording' },
      ] }, seed)
      if (mode === 'possible') return base({ coverage: 'possible', needsTypedConfirm: true, requiresConfirmation: true, providers: [
        { releaseId: 30, title: '本地发行', credit: '专辑艺术家', discNumber: 0, trackNumber: 0, trackTitle: '共同歌曲', durationMs: 200000, evidence: 'text', trackId: 77, inLibrary: true, trackRefs: 2, trackTotal: 0 },
      ] }, seed)
      if (mode === 'unknown') return base({ coverage: 'unknown', needsTypedConfirm: true, requiresConfirmation: true, undecidable: [
        { releaseId: 21, identity: releaseA.identity, title: releaseA.title, credit: releaseA.credit, reason: 'tracklist_incomplete' },
      ] }, seed)
      if (mode === 'conflict') return base({ conflict: true, requiresConfirmation: true, resolution: { groups: [group(1), group(2)],
        evidence: [{ groupId: 1, kind: 'release-current', title: '发行 b', credit: '专辑艺术家' }, { groupId: 2, kind: 'release-snapshot', title: '发行 b', credit: '专辑艺术家' }],
        rejected: [{ groupId: 3, kind: 'release-current', title: '发行 b', credit: '专辑艺术家', reason: 'native_mismatch' }] } }, seed)
      if (mode === 'similar') return base({ resolution: { groups: [], evidence: [], similar: [{ groupId: 9, kind: 'group-current', title: '发行 b', credit: '专辑艺术家' }] } }, seed)
      return base({}, seed)
    }
    api.PrepareAdmission = async (candidate: any, item: number) => { state.candidate = candidate; state.item = item; return w.__makePlan() }
    api.PrepareLocalAdmission = async () => w.__makePlan()
    api.ConfirmReleaseAdditions = async (id: string, hash: string) => { if (state.mode !== 'conflict' && state.mode !== 'membership') throw new Error('typed_confirmation_required'); return { planId: id, hash, kind: '', credential: 'confirmed' } }
    api.ConfirmTypedAdmission = async (id: string, hash: string, word: string) => {
      state.typed.push(word)
      if (word.trim() !== 'Yes') throw new Error('确认词不正确，请输入 Yes')
      return { planId: id, hash, kind: 'typed', credential: 'typed' }
    }
    api.CommitAdmission = async (id: string, hash: string, approval: any) => { state.commits.push({ id, hash, approval }); return { trackId: 90, releaseId: 22 } }
    api.UseSongProvider = async (_id: string, _hash: string, releaseId: number) => { state.used.push(releaseId); return base({ coverage: 'provided', existingReleaseId: releaseId }) }
    api.RefreshAdmissionCoverage = async () => {
      state.refreshed++
      await new Promise(resolve => { w.__finishRefresh = resolve; if (!state.holdRefresh) resolve(null) })
      if (state.refreshFails) throw new Error('上游暂时不可用')
      return w.__makePlan(state.refreshTo)
    }
    api.ReplanAdmissionGroupChoice = async (_id: string, _hash: string, action: string, target: number) => {
      state.choices.push({ action, target })
      return base({ requiresConfirmation: action !== 'separate', resolution: w.__makePlan('conflict').baseline.resolution },
        { mergeTargetId: action === 'merge' ? target : 0, joinGroupId: action === 'join' ? target : 0, keepSeparate: action === 'separate' })
    }
    const queue = api.GetReviewQueue
    api.GetReviewQueue = async () => {
      const q = await queue(); q.items[0].reviewReason = 'song_unresolved'
      const plan = w.__makePlan('provided'); state.plans--
      q.items[0].reviewEvidence = JSON.stringify({ ...plan.baseline, proposed: plan.seed.snapshot.release })
      q.items[0].candidates = [{ provider: 'musicbrainz', matchRef: 'b-5', title: '共同歌曲', album: '发行 B', badges: ['MB'], action: '接受', confidence: 100 }]
      return q
    }
  })
})

const state = (page: Page) => page.evaluate(() => (window as any).__c4)
const setState = (page: Page, patch: Record<string, unknown>) => page.evaluate(patch => Object.assign((window as any).__c4, patch), patch)

async function openFromSearch(page: Page) {
  await page.evaluate(() => {
    ;(window as any).go.app.App.SearchUpstream = async () => ({ byProvider: [{ capabilities: [], rows: [{ provider: 'musicbrainz', matchRef: 'b-5', releaseRef: 'release-b', title: '共同歌曲', badges: ['MB'], action: '加入曲库' }] }] })
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
}

test('review accept: provider details, default use-existing, typed word gates creating the other release', async ({ page }) => {
  await page.locator('nav').getByRole('button', { name: /复核队列/ }).click()
  await expect(page.getByTestId('review-heading').getByTestId('review-reason')).toHaveText('无法确认已有发行是否包含这首歌，需复核')
  await expect(page.getByTestId('review-provider')).toHaveText('可由已有发行提供：已有发行 A · musicbrainz · release-a · 录音相同')
  await page.getByRole('button', { name: '接受', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  const provider = dialog.getByTestId('admission-provider')
  await expect(provider).toContainText('已有发行 A · 专辑艺术家')
  await expect(provider).toContainText('musicbrainz · release-a · 已收藏 4 首 / 共 12 首')
  await expect(provider).toContainText('1-5 · 共同歌曲 · 3:21')
  await expect(provider).toContainText('录音相同')
  // 默认动作是使用已有发行；输入框在选择「创建发行 B」之前不出现。
  await expect(dialog.getByTestId('typed-confirm')).toHaveCount(0)
  await expect(dialog.getByRole('button', { name: '确认成员变化并保存' })).toHaveCount(0)
  await dialog.getByRole('button', { name: '创建发行「发行 B」', exact: true }).click()
  const submit = dialog.getByTestId('typed-submit')
  const input = dialog.getByRole('textbox', { name: /输入 Yes 以确认/ })
  await expect(submit).toBeDisabled()
  for (const wrong of ['yes', 'YES', 'Y']) { await input.fill(wrong); await expect(submit).toBeDisabled() }
  expect((await state(page)).commits).toEqual([])
  await input.fill('Yes')
  await submit.click()
  await expect(dialog).toHaveCount(0)
  const after = await state(page)
  expect(after.typed).toEqual(['Yes'])
  expect(after.item).toBe(11)
  expect(after.commits).toHaveLength(1)
  expect(after.commits[0].approval).toMatchObject({ kind: 'typed', credential: 'typed' })
})

test('search add: using the existing release commits without any confirmation and keeps the candidate on cancel', async ({ page }) => {
  await openFromSearch(page)
  const dialog = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByText('共同歌曲', { exact: true })).toBeVisible()
  expect((await state(page)).commits).toEqual([])
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
  await dialog.getByTestId('use-provider').click()
  await expect(dialog).toHaveCount(0)
  const after = await state(page)
  expect(after.used).toEqual([21])
  expect(after.typed).toEqual([])
  expect(after.commits).toHaveLength(1)
  expect(after.commits[0].approval.credential).toBeUndefined()
})

test('suspected local provider is labelled and reusable', async ({ page }) => {
  await setState(page, { mode: 'possible' })
  await openFromSearch(page)
  const dialog = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  const provider = dialog.getByTestId('admission-provider')
  await expect(provider).toContainText('本地发行')
  await expect(provider).toContainText('疑似：曲名与时长相同')
  await expect(provider).toContainText('已有曲目，将直接复用')
  await expect(dialog.getByTestId('use-provider')).toBeEnabled()
})

test('unknown coverage fetches tracklists once automatically, then offers a retry on failure', async ({ page }) => {
  await setState(page, { mode: 'unknown', holdRefresh: true, refreshFails: true })
  await openFromSearch(page)
  const dialog = page.getByRole('dialog', { name: '无法确认已有发行是否包含这首歌', exact: true })
  await expect(dialog.getByTestId('admission-undecidable')).toContainText('已有发行 A · 专辑艺术家')
  await expect(dialog.getByTestId('admission-undecidable')).toContainText('曲目清单尚未取全')
  await expect(dialog.getByRole('status')).toHaveText('正在获取曲目清单…')
  await page.evaluate(() => (window as any).__finishRefresh(null))
  await expect(dialog).toContainText('获取清单失败：Error: 上游暂时不可用')
  expect((await state(page)).refreshed).toBe(1)
  // 没有可用的提供者时输入框直接可见：仍可输入 Yes 创建。
  await expect(dialog.getByTestId('typed-submit')).toBeDisabled()
  await setState(page, { holdRefresh: false, refreshFails: false, refreshTo: 'provided' })
  await dialog.getByTestId('refresh-coverage').click()
  const resolved = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  await expect(resolved.getByTestId('admission-provider')).toContainText('已有发行 A')
  expect((await state(page)).refreshed).toBe(2)
  expect((await state(page)).commits).toEqual([])
})

test('group conflict offers merge, join-without-merging and keep-separate, and shows rejected evidence', async ({ page }) => {
  await setState(page, { mode: 'conflict' })
  await openFromSearch(page)
  const conflict = page.getByRole('dialog', { name: '归组证据冲突', exact: true })
  await expect(conflict.getByTestId('rejected-evidence')).toHaveText('因 musicbrainz 的发行组不同，未并入专辑组 #3 · 发行 b · 专辑艺术家')
  await expect(conflict.getByRole('button', { name: '预览合并到此组' })).toHaveCount(2)
  await conflict.getByRole('button', { name: '加入此组，不合并其他组' }).nth(1).click()
  const confirm = page.getByRole('dialog', { name: '确认发行成员变化', exact: true })
  await expect(confirm).toContainText('加入专辑组 #2，不合并其他命中的组')
  expect((await state(page)).choices).toEqual([{ action: 'join', target: 2 }])
  await confirm.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
  await conflict.getByTestId('keep-separate').click()
  await expect(page.getByTestId('admission-dialog')).toContainText('保持独立成组，不加入命中的专辑组')
  expect((await state(page)).choices.at(-1)).toEqual({ action: 'separate', target: 0 })
  expect((await state(page)).commits).toEqual([])
})

test('editor save goes through the same typed gate and cannot switch releases in the dialog', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    api.PlanEditSave = async () => w.__makePlan('provided', { isEdit: true })
    api.SaveEdit = async (id: string, hash: string, approval: any) => { w.__c4.editSaves.push({ id, hash, approval }); return { trackId: 1, releaseId: 22 } }
  })
  await page.locator('.track-list [data-row-key]').first().click({ button: 'right' })
  await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
  await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('改过的曲名')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  await expect(dialog.getByTestId('admission-provider')).toContainText('已有发行 A')
  await expect(dialog.getByTestId('use-provider')).toHaveCount(0)
  await expect(dialog).toContainText('当前操作不能在这里改用其他发行')
  const submit = dialog.getByTestId('typed-submit')
  await expect(submit).toBeDisabled()
  await dialog.getByRole('textbox', { name: /输入 Yes 以确认/ }).fill('Yes')
  await submit.click()
  await expect(dialog).toHaveCount(0)
  const after = await state(page)
  expect(after.editSaves).toHaveLength(1)
  expect(after.editSaves[0].approval).toMatchObject({ kind: 'typed' })
  expect(after.commits).toEqual([])
})

test('release merge proposals can be viewed and dismissed', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__merges = [{ id: 5, groupId: 1, trackTotal: 12, status: 'pending',
      a: { releaseId: 21, identity: { provider: 'musicbrainz', ref: 'release-a' }, title: '已有发行 A', trackRefs: 4 },
      b: { releaseId: 22, identity: { provider: 'musicbrainz', ref: 'release-b' }, title: '发行 B', trackRefs: 1 } }]
    api.ListReleaseMergeProposals = async () => structuredClone(w.__merges)
    api.DismissReleaseMergeProposal = async (id: number) => { w.__dismissed = id; w.__merges = []; return null }
  })
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  const proposal = page.getByTestId('release-merge-proposal')
  await expect(proposal).toContainText('发行曲目相同（共 12 首）')
  await expect(proposal).toContainText('已有发行 A（musicbrainz · release-a · 已收藏 4 首）')
  await proposal.getByRole('button', { name: '忽略', exact: true }).click()
  await expect(proposal).toHaveCount(0)
  expect(await page.evaluate(() => (window as any).__dismissed)).toBe(5)
})

test('provider details and the typed input stay fully visible in light, dark and narrow windows', async ({ page }, info) => {
  await openFromSearch(page)
  const dialog = page.getByRole('dialog', { name: '这首歌可由已有发行提供', exact: true })
  await dialog.getByRole('button', { name: '创建发行「发行 B」', exact: true }).click()
  for (const [name, width, dark] of [['light', 1440, false], ['dark', 1440, true], ['narrow', 520, false], ['narrow-dark', 520, true]] as const) {
    await page.setViewportSize({ width, height: 900 })
    await page.evaluate(dark => document.documentElement.classList.toggle('dark', dark), dark)
    const viewport = page.viewportSize()!
    for (const target of [dialog.getByTestId('admission-provider'), dialog.getByRole('textbox', { name: /输入 Yes 以确认/ }), dialog.getByTestId('typed-submit'), dialog.getByTestId('use-provider')]) {
      await target.scrollIntoViewIfNeeded()
      const box = (await target.boundingBox())!
      expect(box.x, `${name}: left edge`).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width, `${name}: right edge`).toBeLessThanOrEqual(viewport.width)
      await expect(target).toBeVisible()
    }
    // 弹窗内容本身不出现横向滚动。
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth + 1), `${name}: no horizontal overflow`).toBe(true)
    await page.screenshot({ path: info.outputPath(`admission-${name}.png`), animations: 'disabled' })
  }
})
