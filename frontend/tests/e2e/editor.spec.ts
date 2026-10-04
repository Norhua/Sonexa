import { test,expect } from './fixtures'
import { fileURLToPath } from 'node:url'
const stub=fileURLToPath(new URL('./wails-stub.js',import.meta.url))
test.beforeEach(async({page})=>{await page.addInitScript({path:stub});await page.goto('/');await page.locator('.track-list [data-row-key]').first().click({button:'right'});await page.getByRole('menuitem',{name:'编辑',exact:true}).click();await expect(page.getByRole('dialog',{name:'编辑曲目',exact:true})).toBeVisible()})
test('use loads complete preview once, shows busy state and keeps drafts after failure', async ({ page }) => {
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('保留手工输入')
 await page.evaluate(() => {
  const api = (window as any).go.app.App, load = api.LoadEditUpstream
  ;(window as any).__loadCount = 0
  api.LoadEditUpstream = (...args: any[]) => new Promise((resolve, reject) => {
   ;(window as any).__loadCount++
   ;(window as any).__failPreview = () => reject(new Error('完整预览获取失败'))
   ;(window as any).__finishPreview = async () => resolve(await load(...args))
  })
 })
 const use = editor.getByRole('button', { name: '使用', exact: true })
 await use.click()
 await expect(use).toHaveAttribute('aria-busy', 'true')
 await expect(use).toBeDisabled()
 await expect(use.locator('.animate-spin')).toBeVisible()
 await page.evaluate(() => (window as any).__failPreview())
 await expect(editor.getByRole('alert')).toContainText('完整预览获取失败')
 await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('保留手工输入')
 await use.click()
 await page.evaluate(() => (window as any).__finishPreview())
 const confirm = page.getByRole('dialog', { name: '使用当前上游信息' })
 await confirm.getByRole('button', { name: '取消', exact: true }).click()
 await use.click()
 await expect.poll(() => page.evaluate(() => (window as any).__loadCount)).toBe(2)
 await confirm.getByRole('button', { name: '确认使用', exact: true }).click()
 await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('上游曲名')
})

test('partial upstream result fails before use confirmation and automatic current cover remains visible', async ({ page }) => {
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 await page.route('**/cover?**', route => route.fulfill({ contentType: 'image/svg+xml', body: '<svg xmlns="http://www.w3.org/2000/svg" width="96" height="96"><rect width="96" height="96" fill="#447788"/></svg>' }))
 await page.evaluate(() => (window as any).runtime.EventsEmit('cover:changed', { releaseId: 20, revision: 1 }))
 await editor.getByLabel('当前封面', { exact: true }).scrollIntoViewIfNeeded()
 const image = editor.getByLabel('当前封面', { exact: true }).locator('img')
 await expect(image).toHaveAttribute('src', /releaseId=20/)
 await expect.poll(() => image.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0)
 await editor.getByRole('button', { name: '清除封面', exact: true }).click()
 await expect(image).toHaveCount(0)
 await editor.getByRole('button', { name: '撤销封面修改', exact: true }).click()
 await expect(image).toBeVisible()
 await page.evaluate(() => {
  const api = (window as any).go.app.App, load = api.LoadEditUpstream
  api.LoadEditUpstream = async (...args: any[]) => ({ ...await load(...args), snapshotJson: JSON.stringify({ release: { tracklistCompleteness: 'partial' } }) })
 })
 await editor.getByRole('button', { name: '使用', exact: true }).click()
 await expect(editor.getByRole('alert')).toContainText('上游暂未提供完整发行清单')
 await expect(page.getByRole('dialog', { name: '使用当前上游信息' })).toHaveCount(0)
 await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
})
test('preview and confirmed use, dirty exit and cancellation',async({page},testInfo)=>{
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await expect(editor.getByRole('textbox',{name:'曲名',exact:true})).toHaveValue('夕立')
 await page.screenshot({path:testInfo.outputPath('editor-desktop.png'),animations:'disabled'})
 await editor.getByRole('button',{name:'搜索候选',exact:true}).click()
 await editor.getByRole('button',{name:/另一首候选/}).click()
 await expect(editor.getByRole('textbox',{name:'曲名',exact:true})).toHaveValue('夕立')
 await editor.getByRole('textbox',{name:'曲名',exact:true}).fill('手工曲名')
 await editor.getByRole('button',{name:'使用',exact:true}).click()
 const confirm=page.getByRole('dialog',{name:'使用当前上游信息'})
 await confirm.getByRole('button',{name:'取消',exact:true}).click()
 await expect(editor.getByRole('textbox',{name:'曲名',exact:true})).toHaveValue('手工曲名')
 await editor.getByRole('button',{name:'使用',exact:true}).click()
 await confirm.getByRole('button',{name:'确认使用',exact:true}).click()
 await expect(editor.getByRole('textbox',{name:'曲名',exact:true})).toHaveValue('另一首候选')
 await editor.getByRole('button',{name:'取消',exact:true}).click()
 await expect(page.getByRole('dialog',{name:'有未保存的修改'})).toBeVisible()
 await page.getByRole('button',{name:'继续编辑',exact:true}).click()
 await expect(editor).toBeVisible()
})
test('explicit per-track migration, duplicate positions and empty positions',async({page})=>{
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await editor.getByRole('textbox',{name:'专辑名',exact:true}).fill('新专辑')
 await editor.getByRole('button',{name:'保存',exact:true}).click()
 const prompt=page.getByRole('dialog',{name:'专辑信息有改动'})
 await expect(prompt.getByRole('button',{name:'修改全部'})).toHaveCount(0)
 await prompt.getByRole('button',{name:'新建专辑',exact:true}).click()
 const impact=page.getByRole('dialog',{name:'确认迁入歌曲'})
 await expect(impact.getByRole('button',{name:'确认影响并继续'})).toBeDisabled()
 await impact.getByRole('button',{name:'查看第二首',exact:true}).click()
 await expect(impact.getByRole('checkbox')).not.toBeChecked()
 await impact.getByRole('checkbox').check()
 await impact.getByRole('button',{name:'查看夕立',exact:true}).click()
 await impact.getByRole('checkbox').check()
 await expect(impact.getByRole('button',{name:'确认影响并继续'})).toBeDisabled()
 await expect(impact.locator('svg[aria-label="所选歌曲位置重复"]')).toHaveCount(2)
 await impact.getByRole('spinbutton',{name:'迁入碟号'}).fill('2')
 await expect(impact.getByRole('button',{name:'确认影响并继续'})).toBeEnabled()
 await impact.getByRole('spinbutton',{name:'迁入音轨号'}).fill('')
 await expect(impact.getByRole('spinbutton',{name:'迁入音轨号'})).toHaveValue('')
 await impact.getByRole('checkbox').uncheck()
 await impact.getByRole('button',{name:'确认影响并继续'}).click()
 await expect(editor).toHaveCount(0)
 expect(await page.evaluate(()=>(window as any).__editDraft.mappings.filter((m:any)=>m.selected).map((m:any)=>m.trackId))).toEqual([2])
})

test('narrow editor keeps editable roles and unsaved input after errors',async({page})=>{
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await page.setViewportSize({width:620,height:760})
 await editor.getByRole('button',{name:'添加演唱者'}).click()
 await editor.getByRole('textbox',{name:'演唱者姓名 1'}).fill('初音未来')
 await editor.getByRole('textbox',{name:'演唱者属性 1'}).fill('合声')
 await editor.getByRole('textbox',{name:'曲名',exact:true}).fill('保留手工值')
 await page.evaluate(()=>{(window as any).go.app.App.PlanEditSave=async()=>{throw new Error('plan_stale 测试')}})
 await editor.getByRole('button',{name:'保存',exact:true}).click()
 await expect(editor.getByRole('alert')).toContainText('plan_stale')
 await expect(editor.getByRole('textbox',{name:'曲名',exact:true})).toHaveValue('保留手工值')
 await editor.getByRole('button',{name:'重新预览',exact:true}).click()
 await expect(editor.getByRole('textbox',{name:'演唱者姓名 1'})).toHaveValue('初音未来')
 const overflow=await editor.evaluate(el=>el.scrollWidth>el.clientWidth+2)
 expect(overflow).toBe(false)
})

test('offline opening keeps text and roles editable without searching',async({page})=>{
 await page.addInitScript(()=>{
  const boot=()=>{const api=(window as any).go?.app?.App;if(api){api.CheckOutbound=async()=>({allowed:false,reason:'离线测试'});api.SearchEditCandidates=async()=>{throw new Error('不应搜索')}}}
  boot();document.addEventListener('DOMContentLoaded',boot)
 })
 await page.reload()
 await page.locator('.track-list [data-row-key]').first().click({button:'right'})
 await page.getByRole('menuitem',{name:'编辑',exact:true}).click()
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await expect(editor.getByRole('button',{name:'搜索候选',exact:true})).toBeDisabled()
 await editor.getByRole('textbox',{name:'曲名',exact:true}).fill('离线曲名')
 await editor.getByRole('button',{name:'添加演唱者'}).click()
 await editor.getByRole('textbox',{name:'演唱者姓名 1'}).fill('初音未来')
})

test('single-track migration still confirms members and cancellation keeps draft',async({page})=>{
 await page.evaluate(()=>{
  const api=(window as any).go.app.App,album=api.PlanAlbumChange,prepare=api.PlanEditSave
  api.PlanAlbumChange=async(draft:any)=>{const p=await album(draft);p.rows=p.rows.slice(0,1);return p}
  api.PlanEditSave=async(draft:any)=>{const p=await prepare(draft);p.baseline.requiresConfirmation=true;return p}
 })
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await editor.getByRole('textbox',{name:'专辑名',exact:true}).fill('单首新专辑')
 await editor.getByRole('button',{name:'保存',exact:true}).click()
 await page.getByRole('button',{name:'新建专辑',exact:true}).click()
 const impact=page.getByRole('dialog',{name:'确认迁入歌曲'})
 await impact.getByRole('checkbox').check()
 await impact.getByRole('button',{name:'确认影响并继续'}).click()
 const member=page.getByRole('dialog',{name:'确认发行成员变化'})
 await expect(member).toBeVisible()
 await page.keyboard.press('Escape')
 await expect(member).toHaveCount(0)
 await expect(impact.getByRole('checkbox')).toBeChecked()
 await impact.getByRole('button',{name:'取消',exact:true}).click()
 await expect(editor.getByRole('textbox',{name:'专辑名',exact:true})).toHaveValue('单首新专辑')
 expect(await page.evaluate(()=>(window as any).__editSaved)).toBeUndefined()
})

test('existing upstream target shows reuse and uses target-list positions',async({page})=>{
 await page.evaluate(()=>{
  const api=(window as any).go.app.App,album=api.PlanAlbumChange
  api.PlanAlbumChange=async(draft:any)=>{const p=await album(draft);p.impact.linksNewUpstream=true;p.impact.preservesTargetValues=true;p.targets=[{ref:'target',title:'目标歌曲',discNumber:2,trackNumber:3}];p.rows=p.rows.slice(0,1).map((r:any)=>({...r,choices:p.targets,after:{...r.after,'release.title':{state:'value',valueJson:'"目标用户标题"'}}}));return p}
 })
 const editor=page.getByRole('dialog',{name:'编辑曲目',exact:true})
 await editor.getByRole('textbox',{name:'专辑名',exact:true}).fill('待变更专辑')
 await editor.getByRole('button',{name:'保存',exact:true}).click()
 await page.getByRole('button',{name:'新建专辑',exact:true}).click()
 const impact=page.getByRole('dialog',{name:'确认迁入歌曲'})
 await expect(impact).toContainText('复用在库发行')
 await expect(impact).toContainText('目标用户标题')
 await expect(impact.getByRole('spinbutton',{name:'迁入碟号'})).toHaveCount(0)
 await impact.getByRole('checkbox').check()
 await expect(impact.getByRole('combobox')).toHaveValue('target')
})

test('binding identifiers are entered on demand and cancellation preserves fields', async ({ page }) => {
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 await expect(editor.getByRole('textbox', { name: '歌曲绑定', exact: true })).toHaveCount(0)
 await expect(editor).not.toContainText('track-1')
 await expect(editor).not.toContainText('release-1')
 await expect(editor.getByRole('button', { name: '全部应用', exact: true })).toHaveCount(0)
 await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('保留当前草稿')
 await editor.getByRole('button', { name: '修改绑定', exact: true }).click()
 const binding = page.getByRole('dialog', { name: '修改上游绑定', exact: true })
 await expect(binding.getByRole('textbox', { name: '歌曲绑定', exact: true })).toHaveValue('')
 await binding.getByRole('textbox', { name: '歌曲绑定', exact: true }).fill('unconfirmed-ref')
 await expect(binding.getByRole('button', { name: '使用', exact: true })).toHaveCount(0)
 await binding.getByRole('button', { name: '取消', exact: true }).click()
 await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('保留当前草稿')
 await editor.getByRole('button', { name: '保存', exact: true }).click()
 await expect(editor).toHaveCount(0)
 expect(await page.evaluate(() => (window as any).__editDraft.bindingAction)).toBe('keep')
})

test('desktop search fields share a row and panels scroll independently', async ({ page }, testInfo) => {
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 const inputRows = await editor.locator('.search-fields input').evaluateAll(inputs => inputs.map(input => input.getBoundingClientRect().y))
 expect(new Set(inputRows).size).toBe(1)
 const upstream = editor.getByRole('region', { name: '上游信息', exact: true })
 const current = editor.getByRole('region', { name: '当前信息', exact: true })
 const edges = await editor.evaluate(el => ({ left: el.querySelector('.upstream-pane')!.getBoundingClientRect().right, right: el.querySelector('.current-pane')!.getBoundingClientRect().left }))
 expect(edges.left).toBeLessThanOrEqual(edges.right + 1)
 await expect(upstream.getByRole('button', { name: '使用', exact: true })).toBeVisible()
 await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
 await current.evaluate(el => { el.scrollTop = el.scrollHeight })
 expect(await current.evaluate(el => el.scrollTop)).toBeGreaterThan(0)
 expect(await upstream.evaluate(el => el.scrollTop)).toBe(0)
 const save = await editor.getByRole('button', { name: '保存', exact: true }).boundingBox()
 expect(save!.y + save!.height).toBeLessThan(900)
 await current.evaluate(el => { el.scrollTop = 0 })
 await page.evaluate(() => document.documentElement.classList.remove('dark'))
 await page.screenshot({ path: testInfo.outputPath('editor-desktop.png'), animations: 'disabled' })
 await page.evaluate(() => document.documentElement.classList.add('dark'))
 await page.screenshot({ path: testInfo.outputPath('editor-dark.png'), animations: 'disabled' })
 await page.setViewportSize({ width: 620, height: 760 })
 await editor.getByRole('textbox', { name: '曲名', exact: true }).scrollIntoViewIfNeeded()
 expect(await editor.evaluate(el => el.scrollWidth > el.clientWidth + 2)).toBe(false)
 await page.screenshot({ path: testInfo.outputPath('editor-narrow.png'), animations: 'disabled' })
})

test('lyrics stay in the draft and Escape closes only the lyrics dialog', async ({ page }, testInfo) => {
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 await editor.getByRole('button', { name: '编辑歌词', exact: true }).click()
 const lyrics = page.getByRole('dialog', { name: '编辑歌词', exact: true })
 await lyrics.getByRole('textbox', { name: '歌词', exact: true }).fill('[00:01.00]保留歌词\n[00:02.00]第二行')
 await lyrics.getByRole('checkbox', { name: '预览正文' }).check()
 await expect(lyrics.locator('pre').first()).toHaveText('保留歌词\n第二行')
 await page.screenshot({ path: testInfo.outputPath('editor-lyrics.png'), animations: 'disabled' })
 await page.keyboard.press('Escape')
 await expect(lyrics).toHaveCount(0)
 await expect(editor).toBeVisible()
 await editor.getByRole('button', { name: '编辑歌词', exact: true }).click()
 await lyrics.getByRole('checkbox', { name: '预览正文' }).uncheck()
 await expect(lyrics.getByRole('textbox', { name: '歌词', exact: true })).toHaveValue('[00:01.00]保留歌词\n[00:02.00]第二行')
 await lyrics.getByRole('button', { name: '完成', exact: true }).click()
 await editor.getByRole('button', { name: '保存', exact: true }).click()
 expect(await page.evaluate(() => JSON.parse((window as any).__editDraft.fields['track.lyrics'].valueJson))).toBe('[00:01.00]保留歌词\n[00:02.00]第二行')
})

test('parsed binding can be changed without replacing current values, then undone', async ({ page }) => {
 await page.evaluate(() => {
  const api = (window as any).go.app.App
  api.LoadEditUpstream = async (...args: any[]) => { (window as any).__bindingArgs = args; return { id: 'manual', snapshotJson: JSON.stringify({release:{tracklistCompleteness:'complete'}}), fields: { 'track.title': { state: 'value', valueJson: '"链接歌曲"' } }, trackIdentity: { provider: 'musicbrainz', ref: 'manual-track' }, releaseIdentity: { provider: 'musicbrainz', ref: 'manual-release' } } }
 })
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 await editor.getByRole('button', { name: '修改绑定', exact: true }).click()
 const binding = page.getByRole('dialog', { name: '修改上游绑定', exact: true })
 await binding.getByRole('textbox', { name: '歌曲绑定', exact: true }).fill('https://musicbrainz.org/track/manual-track')
 await binding.getByRole('button', { name: '解析并预览', exact: true }).click()
 await expect(binding).toContainText('链接歌曲')
 await binding.getByRole('button', { name: '仅更换绑定', exact: true }).click()
 await page.getByRole('dialog', { name: '更换绑定', exact: true }).getByRole('button', { name: '确认使用', exact: true }).click()
 await expect(binding).toHaveCount(0)
 await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('夕立')
 await expect(editor.getByRole('region', { name: '当前信息', exact: true })).toContainText('链接歌曲')
 await editor.getByRole('button', { name: '撤销绑定修改', exact: true }).click()
 await expect(editor.getByRole('region', { name: '当前信息', exact: true })).toContainText('上游曲名')
 await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
 expect(await page.evaluate(() => (window as any).__editSaved)).toBeUndefined()
})

test('snapshot facts, version browsing and cover preview stay separate from the draft', async ({ page }, testInfo) => {
 await page.getByRole('dialog', { name: '编辑曲目', exact: true }).getByRole('button', { name: '取消', exact: true }).click()
 await page.evaluate(() => {
  const api = (window as any).go.app.App, originalSession = api.GetEditSession
  const field = (value: any) => ({ state: 'value', valueJson: JSON.stringify(value), mode: 'follow', source: 'musicbrainz' })
  const fact = (value: any) => ({ state: 'value', value })
  const track = { provider: 'musicbrainz', ref: '9fca2de0-e1fd-4b77-aab5-f3af8c05a437' }
  const release = { provider: 'musicbrainz', ref: '4d23dc11-254b-4df8-9e13-2d15f881cdbd' }
  const group = { provider: 'musicbrainz', ref: '2e251f60-23a6-4a5c-9601-a4b557102831' }
  const rows = [
   { discNumber: 1, trackNumber: 1, title: fact('夕立'), track: fact(track) },
   { discNumber: 1, trackNumber: 2, title: fact('第二首'), track: fact({ provider: 'musicbrainz', ref: 'track-2' }) },
   { discNumber: 2, trackNumber: 1, title: fact('另一个版本的歌曲'), track: fact({ provider: 'musicbrainz', ref: 'track-3' }) },
  ]
  const fields = {
   'track.title': field('夕立'), 'track.credit': field("CROW'SCLAW"), 'track.year': field(2015),
   'track.discNumber': field(1), 'track.trackNumber': field(1), 'track.genre': field('Rock'),
   'track.roles.vocal': field([{ artist: { name: '初音未来', identity: track }, attributes: ['合声'] }]),
   'track.roles.composer': field([{ artist: { name: '作曲艺术家' }, attributes: [] }]),
   'track.lyrics': field('[00:01.00]上游歌词'), 'track.recording': field(track), 'track.works': field([track]),
   'track.identifiers': field([{ kind: 'ISRC', value: 'JPTEST1234567' }]), 'track.durationMS': field(205000),
   'release.title': field('VOCAL COLLECTION'), 'release.credit': field('专辑艺术家'), 'release.primaryType': field('album'),
   'release.country': field('JP'), 'release.date': field('2015-06-10'), 'release.discTotal': field(2),
   'release.media': field([{ number: 1, trackTotal: fact(6), format: fact('CD') }, { number: 2, trackTotal: fact(6), format: fact('CD') }]),
   'release.labels': field([{ name: '测试厂牌', catalogNumber: 'TEST-001', identity: release }]),
   'release.artwork': field([{ identity: release, type: 'front' }]),
  }
  const snapshotJson = JSON.stringify({ group: { identity: group, title: fact('VOCAL COLLECTION'), firstReleaseDate: fact('2010-04-01') }, release: { identity: release, group, tracklist: fact(rows), tracklistCompleteness: 'complete' } })
  const preview = { id: 'rich-preview', snapshotJson, fields, trackIdentity: track, releaseIdentity: release, trackStates: { '1:2': '本版本已收藏', '2:1': '其他版本已收藏' } }
  api.GetEditSession = async (id: number) => { const session = await originalSession(id); Object.assign(session.editor, { upstream: fields, snapshotJson, trackIdentity: track, releaseIdentity: release }); return session }
  api.PreviewCurrentEditUpstream = async () => preview
  api.ListEditVersions = async () => { (window as any).__versionRequests = ((window as any).__versionRequests || 0) + 1; return { total: 1, items: [{ ref: 'alternative-release', title: '另一发行版本', country: 'JP', trackTotal: 15 }] } }
  api.StageEditUpstreamCover = async () => { (window as any).__coverStages = ((window as any).__coverStages || 0) + 1; return { token: 'staged-preview-cover', previewUrl: 'data:image/svg+xml,' + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" width="120" height="120"><rect width="120" height="120" fill="#737373"/><circle cx="60" cy="60" r="35" fill="#d4d4d4"/></svg>') } }
 })
 await page.locator('.track-list [data-row-key]').first().click({ button: 'right' })
 await page.getByRole('menuitem', { name: '编辑', exact: true }).click()
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 const upstream = editor.getByRole('region', { name: '上游信息', exact: true })
 expect(await page.evaluate(() => (window as any).__versionRequests)).toBeUndefined()
 await upstream.getByRole('button', { name: '浏览版本', exact: true }).click()
 await expect(upstream.getByRole('button', { name: /另一发行版本/ })).toBeVisible()
 expect(await page.evaluate(() => (window as any).__versionRequests)).toBe(1)
 await upstream.getByRole('button', { name: '查看上游封面', exact: true }).click()
 await expect(upstream.getByRole('img', { name: '上游封面预览', exact: true })).toBeVisible()
 await expect(editor.getByRole('button', { name: '保存', exact: true })).toBeDisabled()
 await upstream.getByRole('button', { name: '展开当前发行清单', exact: true }).click()
 await expect(upstream).toContainText('本曲')
 await expect(upstream).toContainText('本版本已收藏')
 await expect(upstream).toContainText('其他版本已收藏')
 await expect(upstream).toContainText('第 2 碟')
 const current = editor.getByRole('region', { name: '当前信息', exact: true })
 await current.locator('summary').click()
 await expect(current).toContainText('ISRC：JPTEST1234567')
 await expect(current).toContainText('测试厂牌 · TEST-001')
 await expect(current).toContainText('3:25')
 await expect(editor).not.toContainText('9fca2de0-e1fd-4b77-aab5-f3af8c05a437')
 await expect(editor).not.toContainText('4d23dc11-254b-4df8-9e13-2d15f881cdbd')
 await expect(editor).not.toContainText('2e251f60-23a6-4a5c-9601-a4b557102831')
 await page.evaluate(() => document.documentElement.classList.remove('dark'))
 await page.screenshot({ path: testInfo.outputPath('editor-album-facts.png'), animations: 'disabled' })
 await upstream.getByRole('button', { name: '使用', exact: true }).click()
 await page.getByRole('dialog', { name: '使用当前上游信息', exact: true }).getByRole('button', { name: '确认使用', exact: true }).click()
 await expect(editor.getByRole('textbox', { name: '专辑名', exact: true })).toHaveValue('VOCAL COLLECTION')
 await expect(editor.getByRole('img', { name: '封面草稿', exact: true })).toBeVisible()
 expect(await page.evaluate(() => (window as any).__coverStages)).toBe(1)
 expect(await page.evaluate(() => (window as any).__editSaved)).toBeUndefined()
})


test('refreshing the bound upstream keeps hand edits and the left candidate preview', async ({ page }) => {
 await page.evaluate(() => {
  const api = (window as any).go.app.App
  api.LoadEditUpstream = async (source: string, release: string, track: string) => {
   (window as any).__refreshedBinding = { source, release, track }
   return { id: 'refreshed-binding', snapshotJson: '{}', fields: { 'track.title': { state: 'value', valueJson: '"刷新后的上游曲名"' } }, trackIdentity: { provider: source, ref: track }, releaseIdentity: { provider: source, ref: release } }
  }
 })
 const editor = page.getByRole('dialog', { name: '编辑曲目', exact: true })
 const upstream = editor.getByRole('region', { name: '上游信息', exact: true })
 const current = editor.getByRole('region', { name: '当前信息', exact: true })
 await upstream.getByRole('button', { name: '搜索候选', exact: true }).click()
 await upstream.getByRole('button', { name: /另一首候选/ }).click()
 await editor.getByRole('textbox', { name: '曲名', exact: true }).fill('保留手工值')
 await current.getByRole('button', { name: '重新获取', exact: true }).click()
 await expect(current).toContainText('刷新后的上游曲名')
 await expect(upstream).toContainText('另一首候选')
 await expect(editor.getByRole('textbox', { name: '曲名', exact: true })).toHaveValue('保留手工值')
 expect(await page.evaluate(() => (window as any).__refreshedBinding)).toEqual({ source: 'musicbrainz', release: 'release-1', track: 'track-1' })
 expect(await page.evaluate(() => (window as any).__editSaved)).toBeUndefined()
})
