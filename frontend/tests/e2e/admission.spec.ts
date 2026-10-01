import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__commits = []; w.__plans = 0
    const groups = [1, 2].map(id => ({ id, title: `专辑组 ${id}`, credit: '专辑艺术家', members: [{ id: id + 10, title: `已有发行 ${id}`, identity: { provider: 'musicbrainz', ref: `release-${id}` }, trackIds: [id] }] }))
    w.__makePlan = () => ({ id: `plan-${++w.__plans}`, hash: 'hash', seed: { snapshot: { release: { identity: { provider: 'qq', ref: 'new-release' }, title: { state: 'value', value: '拟加入版本' }, credit: { state: 'value', value: { text: '专辑艺术家' } } } } }, baseline: { conflict: !!w.__conflict, requiresConfirmation: true, resolution: { groups: w.__conflict ? groups : [groups[0]], evidence: [{ groupId: 1, kind: 'name', title: '拟加入版本', credit: '专辑艺术家' }] } } })
    api.PrepareLocalAdmission = async (input: any) => { w.__input = input; return w.__makePlan() }
    api.PrepareAdmission = async (candidate: any, item: number) => { w.__candidate = candidate; w.__item = item; return w.__makePlan() }
    api.PrepareNoMatchNeeded = async (item: number) => { w.__item = item; return w.__makePlan() }
    api.ConfirmReleaseAdditions = async (id: string, hash: string) => ({ planId: id, hash, credential: 'confirmed' })
    api.CommitAdmission = async (id: string, hash: string, approval: any) => { if (w.__stale) { w.__stale = false; throw new Error('plan_stale: 资料已更新') }; w.__commits.push({ id, hash, approval }); return { trackId: 90 } }
    api.ReplanAdmissionGroup = async (_id: string, _hash: string, target: number) => { const next = w.__makePlan(); next.baseline.conflict = false; next.seed.mergeTargetId = target; return next }
    api.PrepareGroupProposal = async () => w.__makePlan()
    api.ListGroupProposals = async () => [{ id: 8, releaseId: 20, snapshot: { release: { title: { value: '迟到资料' } } }, resolution: { groups } }]
    const queue = api.GetReviewQueue
    api.GetReviewQueue = async () => {
      const q = await queue(); q.items[0].inTitle = ''; q.items[0].reviewReason = 'new_release'
      q.items[0].reviewEvidence = JSON.stringify({ ...w.__makePlan().baseline, proposed: w.__makePlan().seed.snapshot.release })
      q.items[0].candidates = [{ provider: 'qq', matchRef: 'candidate', title: '候选歌曲', album: '拟加入版本', badges: ['QQ'], action: '接受', confidence: 100 }]
      return q
    }
  })
})

test('local blank fields, nested cancel, stale plan repreview and bounded approval', async ({ page }) => {
  await page.getByRole('button', { name: '新建曲目', exact: true }).click()
  const editor = page.getByRole('dialog', { name: '新建曲目', exact: true })
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: '确认发行成员变化', exact: true })
  await expect(confirm).toContainText('已有发行 1')
  await expect(confirm).toContainText('拟加入版本')
  await page.keyboard.press('Escape')
  await expect(confirm).toHaveCount(0)
  await expect(editor).toBeVisible()
  expect(await page.evaluate(() => (window as any).__commits)).toEqual([])
  await editor.getByLabel('曲名', { exact: true }).fill('手工曲名')
  await editor.getByRole('button', { name: '保存', exact: true }).click()
  await page.evaluate(() => { (window as any).__stale = true })
  await confirm.getByRole('button', { name: '确认成员变化并保存' }).click()
  await expect(confirm).toContainText('plan_stale')
  await confirm.getByRole('button', { name: '重新预览' }).click()
  await confirm.getByRole('button', { name: '确认成员变化并保存' }).click()
  await expect(editor).toHaveCount(0)
  const result = await page.evaluate(() => ({ input: (window as any).__input, commits: (window as any).__commits }))
  expect(result.input).toMatchObject({ title: '手工曲名', artist: null, album: null, albumArtist: null })
  expect(result.commits).toHaveLength(1)
  expect(result.commits[0].approval.planId).toBe(result.commits[0].id)
})

test('review shows missing fields and new release evidence; accepting and no-match cancellation keep the item', async ({ page }) => {
  await page.getByRole('button', { name: /复核队列/ }).click()
  await expect(page.getByText('部分基本信息缺失', { exact: false })).toBeVisible()
  await expect(page.getByText('拟加入发行：', { exact: false })).toContainText('拟加入版本')
  await page.getByRole('button', { name: '接受', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: '确认发行成员变化' })
  await confirm.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('button', { name: '接受', exact: true })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__item)).toBe(11)
  await page.getByRole('button', { name: '无需匹配', exact: true }).click()
  await expect(confirm).toBeVisible()
  await confirm.getByRole('button', { name: '取消', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__commits)).toEqual([])
})

test('proposal conflict previews all members and requires separate final confirmation', async ({ page }) => {
  await page.evaluate(() => { (window as any).__conflict = true })
  await page.getByRole('button', { name: '专辑', exact: true }).click()
  await page.getByRole('button', { name: '查看归组提议' }).click()
  const conflict = page.getByRole('dialog', { name: '归组证据冲突' })
  await expect(conflict).toContainText('已有发行 1')
  await expect(conflict).toContainText('已有发行 2')
  await conflict.getByRole('button', { name: '预览合并到此组' }).nth(1).click()
  const confirm = page.getByRole('dialog', { name: '确认发行成员变化' })
  await expect(confirm).toContainText('合并至专辑组 #2')
  expect(await page.evaluate(() => (window as any).__commits)).toEqual([])
  await confirm.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByRole('button', { name: '查看归组提议' })).toBeVisible()
})

test('search cancellation preserves selected candidate and successful confirmation updates it', async ({ page }) => {
  await page.evaluate(() => {
    ;(window as any).go.app.App.SearchUpstream = async () => ({ byProvider: [{ capabilities: [], rows: [{ provider: 'musicbrainz', matchRef: 'explicit-version', releaseRef: 'new-release', title: '明确选择的候选', badges: ['MB'], action: '加入曲库' }] }] })
  })
  await page.locator('nav').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('combobox').click()
  await page.getByRole('option', { name: 'MusicBrainz', exact: true }).click()
  await page.locator('main').getByRole('button', { name: '搜索', exact: true }).click()
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
  const confirm = page.getByRole('dialog', { name: '确认发行成员变化' })
  await confirm.getByRole('button', { name: '取消', exact: true }).click()
  await expect(page.getByText('明确选择的候选', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '加入曲库', exact: true }).click()
  await confirm.getByRole('button', { name: '确认成员变化并保存' }).click()
  await expect(page.getByRole('button', { name: '查看库中曲目：明确选择的候选' })).toBeVisible()
  expect(await page.evaluate(() => (window as any).__candidate.matchRef)).toBe('explicit-version')
})

test('import preview separates missing title from skipped empty row and submits importable rows', async ({ page }) => {
  await page.evaluate(() => {
    const api = (window as any).go.app.App
    api.PickImportFile = async () => '/tmp/missing.csv'
    api.AnalyzeImportFile = async () => ({ detection: { needsAttention: false, encoding: 'UTF-8', delimiterRune: ',', hasHeader: true }, statusLine: 'CSV · UTF-8', columnMap: ['title', 'artist'], headers: ['曲名', '艺术家'], fieldOptions: [], preview: { totalRows: 2, importableRows: 1, skippedRows: 1, skipSummary: '跳过空行 1 行', missingSummary: '缺少曲名 1 行，仍将导入并可逐条复核', rows: [{ title: '', artist: '已知艺术家', skipReason: '' }, { title: '', skipReason: '空行' }] } })
    api.SubmitImport = async (...args: any[]) => { (window as any).__submitted = args; return 1 }
  })
  await page.getByRole('button', { name: /导入队列/ }).click()
  await page.getByRole('button', { name: '导入表格', exact: true }).click()
  await page.getByRole('button', { name: '选择文件', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '导入 missing.csv' })
  await expect(dialog).toContainText('缺少曲名 1 行，仍将导入')
  await expect(dialog).toContainText('跳过空行 1 行')
  await expect(dialog.getByText('已知艺术家')).toBeVisible()
  await dialog.getByRole('button', { name: '提交导入 (1 条)', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__submitted?.[0])).toBe('/tmp/missing.csv')
})
