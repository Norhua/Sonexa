import { test, expect } from './fixtures'
import { fileURLToPath } from 'node:url'

test.beforeEach(async ({ page }) => {
  await page.addInitScript({ path: fileURLToPath(new URL('./wails-stub.js', import.meta.url)) })
  await page.goto('/')
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App, original = api.GetTrackDetail
    w.__lyricText = ''
    api.GetTrackDetail = async (id: number) => ({ ...await original(id), metadata: { lyrics: id === 1 ? w.__lyricText : '' } })
    api.PickAndAdoptLyrics = async (id: number, revision: number) => {
      w.__lyricPick = { id, revision }; w.__lyricText = '[ti:元信息]\n[00:01.00]<00:01.00>第一<00:02.00>行\n[00:03.00]第二行'
      w.runtime.EventsEmit('library:changed'); return { cancelled: false, revision: 1 }
    }
    api.ClearLyrics = async (id: number, revision: number) => { w.__lyricClear = { id, revision }; w.__lyricText = ''; w.runtime.EventsEmit('library:changed'); return 2 }
  })
  await page.locator('.track-list [data-row-key="track:1"] td').last().dblclick()
})

test('local lyrics, independent lyric page, clear and resizable indented drawer', async ({ page }, testInfo) => {
  const drawer = page.getByLabel('歌曲详情抽屉'), icon = drawer.getByRole('button', { name: /查看歌词/ })
  await expect(icon).toHaveAttribute('data-has-lyrics', 'false')
  await expect(icon).toHaveClass(/text-black/)
  await expect(drawer.locator('summary')).toHaveCount(0)
  const fields = drawer.locator('.detail-fields').first()
  expect(await fields.evaluate(el => getComputedStyle(el).paddingLeft)).toBe('12px')
  await icon.click()
  await expect(drawer.getByText('暂无歌词，请返回详情后右键歌词图标添加。')).toBeVisible()
  await expect(drawer.getByRole('button', { name: '关闭', exact: true })).toHaveCount(0)
  await drawer.getByRole('button', { name: '返回详情' }).click()
  await icon.focus(); await page.keyboard.press('Shift+F10')
  await page.getByRole('menuitem', { name: '从本地选择歌词' }).click()
  await expect(icon).toHaveAttribute('data-has-lyrics', 'true')
  await expect(icon).toHaveClass(/text-green-600/)
  expect(await page.evaluate(() => (window as any).__lyricPick.id)).toBe(1)
  const edge = drawer.getByRole('separator', { name: '调整详情抽屉宽度' })
  const initial = (await drawer.boundingBox())!.width
  const bounds = (await edge.boundingBox())!
  await page.mouse.move(bounds.x + 4, bounds.y + 200); await page.mouse.down(); await page.mouse.move(bounds.x - 116, bounds.y + 200); await page.mouse.up()
  await expect.poll(async () => (await drawer.boundingBox())!.width).toBe(initial + 120)
  await edge.focus(); await page.keyboard.press('ArrowLeft')
  await expect.poll(async () => (await drawer.boundingBox())!.width).toBe(initial + 140)
  await icon.click()
  await expect(drawer.getByLabel('歌曲歌词')).toContainText('第一行\n第二行')
  await expect(drawer.getByLabel('歌曲歌词')).not.toContainText('00:01')
  await page.screenshot({ path: testInfo.outputPath('lyrics-drawer.png') })
  await drawer.getByRole('button', { name: '返回详情' }).click()
  await icon.click({ button: 'right' }); await page.getByRole('menuitem', { name: '清除歌词' }).click()
  await page.getByRole('dialog').getByRole('button', { name: '清除', exact: true }).click()
  await expect(icon).toHaveAttribute('data-has-lyrics', 'false')
  expect(await page.evaluate(() => (window as any).__lyricClear.id)).toBe(1)
})

test('two-column lyric search previews source, duration and format, handles source failure and applies to the song', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string) => {
      if (source === 'kuwo') throw new Error('酷我暂时限流')
      return [{ id: source, source, sortKey: source, title: `歌曲-${source}`, artist: '歌手', album: '专辑', durationMs: 123000, matchScore: 80, totalScore: 125, matchTerms: [{ code: 'title', label: '曲名', value: 55 }], scoreTerms: [{ code: 'translation', label: '翻译', value: 15 }], highConfidence: false, autoPreviewEligible: false, matchNotes: ['艺术家不同或缺失，需确认'] }]
    }
    w.__lyricDocument = (id: string) => ({ text: id === 'qq' ? '[00:12.000]<00:12.100>Hello <00:12.600>world<00:13.000>\n[00:12.000]测试翻译\n<b>literal</b>' : '纯文本歌词', format: id === 'qq' ? 'enhanced_lrc' : 'text', offsetMs: 0, rows: [{ timeMs: 12000, text: 'Hello world', channel: 'original', translation: ['测试翻译'] }, { text: '<b>literal</b>', channel: 'original' }], translation: { status: 'available', origin: 'independent', syncStatus: 'synced' }, romanization: { status: 'unavailable', origin: 'independent', syncStatus: 'unknown' }, wordTiming: 'available' })
    w.__lyricAcknowledge = () => new Promise<void>(resolve => { w.__finishLyricsPreview = resolve })
    const adopt = api.AdoptLyrics
    api.AdoptLyrics = async (session: string, id: string, revision: number) => {
      await adopt(session, id, revision)
      w.__lyricAdopt = { session, id, revision }; w.__lyricText = w.__lyricDocument(id).text; w.runtime.EventsEmit('library:changed'); return 1
    }
  })
  const drawer = page.getByLabel('歌曲详情抽屉')
  await drawer.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await expect(dialog.getByText('仅修改当前歌曲的歌词', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  const qq = dialog.getByRole('button', { name: /歌曲-qq/ })
  await expect(qq).toContainText('匹配：需确认')
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeDisabled()
  await expect(dialog.getByLabel('歌词预览').locator('pre')).toHaveCount(0)
  await expect(qq).toContainText('翻译：有')
  await expect(qq).toContainText('罗马音：未提供')
  await qq.click()
  await expect(qq).toContainText('来源：QQ 音乐 · 时长：2:03')
  await expect(qq).toContainText('类型：增强型 LRC')
  await expect(dialog.getByLabel('可读歌词')).toContainText('Hello world')
  await expect(dialog.getByLabel('可读歌词')).not.toContainText('<00:12.100>')
  await expect(dialog.getByLabel('可读歌词')).toContainText('<b>literal</b>')
  await expect(dialog.getByLabel('可读歌词').locator('b')).toHaveCount(0)
  await expect(dialog.getByLabel('歌词通道状态')).toContainText('翻译：有')
  await expect(dialog.getByLabel('歌词通道状态')).toContainText('罗马音：未提供')
  await expect(dialog.getByLabel('歌词通道状态')).toContainText('逐字时间：已获取')
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeDisabled()
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLyricsPreview)).toBe('function')
  await page.evaluate(() => (window as any).__finishLyricsPreview())
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeEnabled()
  await dialog.getByRole('button', { name: '原始文本', exact: true }).focus()
  await page.keyboard.press('Enter')
  await expect(dialog.getByLabel('原始歌词文本')).toContainText('<00:12.100>Hello')
  await expect(dialog.getByLabel('原始歌词文本')).toContainText('测试翻译')
  await dialog.getByRole('button', { name: '阅读', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__lyricReads)).toBe(1)
  await expect(dialog.getByText('酷我暂时限流', { exact: false })).toBeVisible()
  const left = (await dialog.getByLabel('歌词候选').boundingBox())!, right = (await dialog.getByLabel('歌词预览').boundingBox())!
  expect(left.x + left.width).toBeLessThanOrEqual(right.x + 1)
  await page.screenshot({ path: testInfo.outputPath('lyrics-search.png') })
  await dialog.getByRole('button', { name: '应用到歌曲' }).click()
  await expect(dialog.getByText('歌词已应用到当前歌曲')).toBeVisible()
  expect(await page.evaluate(() => (window as any).__lyricAdopt.id)).toBe('qq')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(drawer.getByRole('button', { name: /查看歌词/ })).toHaveAttribute('data-has-lyrics', 'true')
  await drawer.getByRole('button', { name: /查看歌词/ }).click()
  await expect(drawer.getByLabel('歌曲歌词')).toContainText('Hello world')
  await expect(drawer.getByLabel('歌曲歌词')).not.toContainText('<00:12.100>')
})

test('cross-source content ranking and unselected badges stay versioned while focus survives reordering', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.__lyricSearch = async (source: string) => {
      const rows = source === '163' ? [['a', 145, 90], ['c', 115, 100], ['e', 110, 80], ['pending', null, 70]] : source === 'qq' ? [['b', 130, 95]] : []
      return rows.map(([id, totalScore, matchScore]) => ({ id, source, sortKey: id, title: `候选-${id}`, artist: 'Artist', album: 'Album', durationMs: 90000, matchScore, totalScore, highConfidence: false, autoPreviewEligible: false, matchNotes: [], matchTerms: [], scoreTerms: [{ code: 'translation', label: '翻译', value: id === 'a' ? 15 : 0 }] }))
    }
    w.__lyricDocument = (id: string) => ({ text: '[00:01]Original\n[00:01]译文\n' + 'long '.repeat(400), format: 'lrc', offsetMs: 0, wordTiming: 'unavailable', rows: [{ timeMs: 1000, text: 'Original', channel: 'original', translation: ['译文'] }, { text: 'long '.repeat(400), channel: 'original' }], translation: { status: id === 'a' ? 'available' : id === 'b' ? 'error' : 'unknown', origin: 'embedded', syncStatus: id === 'a' ? 'partial' : 'unknown', diagnostics: id === 'a' ? [{ code: 'embedded_translation', message: '启发式识别', inference: { ruleVersion: 'synthetic-v1', layer: 1, count: 8, associations: [] } }] : [] }, romanization: { status: id === 'b' ? 'available' : 'unavailable', origin: 'independent', syncStatus: id === 'b' ? 'unsynced' : 'unknown' } })
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' }), list = dialog.getByLabel('歌词候选')
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(list.locator('[data-candidate-id]')).toHaveCount(5)
  expect(await list.locator('[data-candidate-id]').evaluateAll(els => els.map(e => e.getAttribute('data-candidate-id')))).toEqual(['a', 'b', 'c', 'e', 'pending'])
  await expect(list.locator('h3')).toHaveCount(0)
  await expect(list.locator('[data-candidate-id="pending"]')).toContainText('歌词综合 —')
  await expect(list.locator('[data-candidate-id="pending"]')).toContainText('未预取，手动获取')
  await expect(list.locator('[data-candidate-id="pending"]')).toContainText('翻译：待检查')
  await expect(list.locator('[data-candidate-id="a"]')).toContainText('翻译：有（启发式识别、部分）')
  await expect(list.locator('[data-candidate-id="b"]')).toContainText('翻译：失败')
  await expect(list.locator('[data-candidate-id="b"]')).toContainText('罗马音：有（未同步）')
  await expect(list.locator('[data-candidate-id="c"]')).toContainText('翻译：无法确认')
  await expect(dialog.getByLabel('歌词正文')).toHaveCount(0)
  await list.locator('[data-candidate-id="a"] summary').click()
  await expect(list.locator('[data-candidate-id="a"]')).toContainText('综合 · 翻译：15')
  const selected = list.getByRole('button', { name: /候选-c/ })
  await selected.click(); await selected.focus()
  await page.evaluate(() => {
    const w = window as any, event = structuredClone(w.__lyricProgress.qq)
    event.sequence = 10; event.candidates[0].candidate.totalScore = 150
    w.runtime.EventsEmit('lyrics:progress', event)
  })
  expect(await list.locator('[data-candidate-id]').evaluateAll(els => els.map(e => e.getAttribute('data-candidate-id')))).toEqual(['b', 'a', 'c', 'e', 'pending'])
  await expect(selected).toBeFocused(); await expect(selected).toHaveAttribute('aria-pressed', 'true')
  await expect(dialog.getByLabel('可读歌词')).toContainText('long long')
  expect(await dialog.getByLabel('歌词预览').evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.evaluate(() => {
    const w = window as any, event = structuredClone(w.__lyricProgress.qq)
    event.sequence = 9; event.candidates[0].translation.status = 'available'; event.candidates[0].candidate.totalScore = 0
    w.runtime.EventsEmit('lyrics:progress', event)
  })
  await expect(list.locator('[data-candidate-id="b"]')).toContainText('翻译：失败')
  await expect(list.locator('[data-candidate-id="b"]')).toContainText('歌词综合 150')
  await list.getByRole('button', { name: /候选-pending/ }).click()
  await expect(dialog.getByText('未预取此候选，请手动获取歌词。')).toBeVisible()
  expect(await page.evaluate(() => (window as any).__lyricReads)).toBe(1)
  await dialog.getByRole('button', { name: '手动获取歌词' }).click()
  await expect(dialog.getByLabel('可读歌词')).toContainText('long long')
  expect(await page.evaluate(() => (window as any).__lyricReads)).toBe(2)
})

test('channel badges distinguish pending and loading before any selection', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string) => source === 'qq' ? [{ id: 'queued', source, sortKey: '01', title: '候选-queued', artist: '', album: '', durationMs: 1000, matchScore: 80, totalScore: null, matchTerms: [], scoreTerms: [], highConfidence: false, autoPreviewEligible: false, matchNotes: [] }] : []
    const start = api.StartLyricsPrefetch, snapshot = api.LyricsSearchSnapshot
    api.StartLyricsPrefetch = async (id: string) => {
      const event = await start(id)
      if (event.source === 'qq') {
        event.sequence = 10; event.phase = 'prefetching'
        Object.assign(event.candidates[0], { fetchState: 'loading', documentVersion: 0, translation: { status: 'loading' }, romanization: { status: 'pending' } })
        w.runtime.EventsEmit('lyrics:progress', event)
      }
      return event
    }
    api.LyricsSearchSnapshot = async (id: string) => snapshot(id)
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  const card = dialog.getByRole('button', { name: /候选-queued/ })
  await expect(card).toContainText('翻译：获取中')
  await expect(card).toContainText('罗马音：待检查')
  await expect(card).toHaveAttribute('aria-pressed', 'false')
  await expect(dialog.getByLabel('歌词正文')).toHaveCount(0)
})

test('a lyrics-only registry drives both visible choices and default dispatch', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    api.ListLyricsSources = async () => [{ id: 'lyrics-only', name: '独立歌词' }]
    w.__lyricsRequests = []
    w.__lyricSearch = async (source: string) => { w.__lyricsRequests.push(source); return [] }
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await expect(dialog.getByRole('checkbox')).toHaveCount(1)
  await expect(dialog.getByRole('checkbox', { name: '独立歌词' })).toBeChecked()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__lyricsRequests)).toEqual(['lyrics-only'])
  await expect(dialog.getByText('无匹配歌曲', { exact: false })).toBeVisible()
})

test('closing while a lyric search starts discards late results and cancels the backend session', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__cancelledLyrics = []
    api.CreateLyricsSearch = () => new Promise(resolve => { w.__finishLyricsStart = () => resolve('late-session') })
    api.CancelLyricsSearch = async (id: string) => { w.__cancelledLyrics.push(id) }
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  for (const name of ['网易云音乐', '酷狗音乐', '酷我音乐']) await dialog.getByText(name, { exact: true }).click()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLyricsStart)).toBe('function')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.evaluate(() => (window as any).__finishLyricsStart())
  await expect.poll(() => page.evaluate(() => (window as any).__cancelledLyrics)).toContain('late-session')
  await expect(dialog).toHaveCount(0)
})
