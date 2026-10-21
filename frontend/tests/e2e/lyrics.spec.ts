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

test('compact lyric search keeps metadata on cards, hides diagnostics and applies the previewed document', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string, query: unknown) => {
      w.__submittedLyricQuery = query
      if (source === 'kugou') throw new Error('酷狗暂时限流')
      return [{ id: source, source, sortKey: source, title: `歌曲-${source}`, artist: '歌手', album: '专辑', durationMs: 123000, matchScore: 80, totalScore: 125, matchTerms: [{ code: 'title', label: '曲名', value: 55 }], scoreTerms: [{ code: 'translation', label: '翻译', value: 15 }], highConfidence: false, autoPreviewEligible: false, matchNotes: ['艺术家不同或缺失，需确认'] }]
    }
    w.__lyricDocument = (id: string) => ({ text: id === 'qq' ? '[00:12.000]<00:12.100>Hello <00:12.600>world<00:13.000>\n[00:12.000]测试翻译\n<b>literal</b>' : '纯文本歌词', format: id === 'qq' ? 'enhanced_lrc' : 'text', offsetMs: 0, rows: [{ timeMs: 12000, text: 'Hello world', channel: 'original', translation: ['测试翻译'] }, { text: '<b>literal</b>', channel: 'original' }], translation: { status: 'available', origin: 'independent', syncStatus: 'synced' }, romanization: { status: 'unavailable', origin: 'independent', syncStatus: 'unknown' }, wordTiming: 'available', warnings: ['内部协议诊断不应显示'] })
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
  await expect(dialog.getByText(/目标：|仅修改当前歌曲的歌词/)).toHaveCount(0)
  await expect(dialog.getByRole('combobox', { name: '额外展示' })).toHaveCount(0)
  const duration = dialog.getByRole('textbox', { name: '时长', exact: true })
  await duration.fill('4:59.123')
  const artistBox = (await dialog.getByRole('textbox', { name: '艺术家', exact: true }).boundingBox())!
  expect((await duration.boundingBox())!.x).toBeGreaterThan(artistBox.x + artistBox.width)
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  const qq = dialog.getByRole('button', { name: /歌曲-qq/ })
  const score = dialog.locator('[data-candidate-id="qq"]').getByRole('button', { name: '综合得分 125/150', exact: true })
  await expect(score).toHaveText('125/150')
  await expect(qq).not.toContainText('匹配：需确认')
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  await score.hover()
  await expect(page.getByRole('tooltip')).toContainText('歌曲匹配 80 分・歌词综合 125，艺术家不同或缺失，需确认')
  await page.getByRole('tooltip').hover()
  await expect(page.getByRole('tooltip')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('lyrics-score-tooltip.png') })
  await qq.focus()
  await score.focus()
  await expect(page.getByRole('tooltip')).toContainText('需确认')
  await page.keyboard.press('Escape')
  await expect(dialog).toBeVisible()
  await expect(page.getByRole('tooltip')).toHaveCount(0)
  const cardBox = (await dialog.locator('[data-candidate-id="qq"]').boundingBox())!, scoreBox = (await score.boundingBox())!
  expect(scoreBox.x).toBeGreaterThan(cardBox.x + cardBox.width / 2)
  expect(scoreBox.y - cardBox.y).toBeLessThan(20)
  expect(await page.evaluate(() => (window as any).__submittedLyricQuery.durationMs)).toBe(299123)
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：共 1 个候选', exact: true })).toHaveText('1')
  await expect(dialog.getByRole('status', { name: '酷狗音乐：获取异常，可重试', exact: true }).locator('svg')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeDisabled()
  await expect(dialog.getByLabel('歌词预览', { exact: true }).locator('pre')).toHaveCount(0)
  await expect(qq).toContainText('翻译：有')
  await expect(qq).toContainText('罗马音：未提供')
  await qq.click()
  await expect(dialog.getByLabel('歌词预览工具栏').getByRole('combobox', { name: '额外展示' })).toHaveText('翻译')
  await expect(dialog.locator('header').getByRole('combobox')).toHaveCount(0)
  await expect(qq).toContainText('来源：QQ 音乐 · 时长：2:03')
  await expect(qq).toContainText('类型：增强型 LRC')
  await expect(dialog.getByLabel('可读歌词')).toContainText('Hello world')
  await expect(dialog.getByLabel('可读歌词')).not.toContainText('<00:12.100>')
  await expect(dialog.getByLabel('可读歌词')).toContainText('<b>literal</b>')
  await expect(dialog.getByLabel('可读歌词').locator('b')).toHaveCount(0)
  await expect(dialog.getByLabel('歌词通道状态')).toHaveCount(0)
  await expect(dialog.getByLabel('歌词预览', { exact: true }).getByRole('heading')).toHaveCount(0)
  await expect(dialog.getByLabel('歌词预览', { exact: true })).not.toContainText('歌曲-qq')
  await expect(dialog.getByLabel('歌词预览', { exact: true })).not.toContainText('增强型 LRC')
  await expect(dialog).not.toContainText('内部协议诊断不应显示')
  await expect(dialog.getByLabel('可读歌词').getByRole('columnheader')).toHaveText(['时间', '歌词', '翻译'])
  const cells = dialog.getByLabel('可读歌词').getByRole('row').nth(1).getByRole('cell')
  await expect(cells).toHaveText(['00:12.000', 'Hello world', '测试翻译'])
  const positions = await cells.evaluateAll(els => els.map(el => { const box = el.getBoundingClientRect(); return { left: box.left, right: box.right, top: box.top } }))
  expect(positions[0]!.right).toBeLessThan(positions[1]!.left)
  expect(positions[1]!.right).toBeLessThan(positions[2]!.left)
  expect(positions[0]!.top).toBe(positions[2]!.top)
  const typography = await cells.evaluateAll(els => els.map(el => {
    const style = getComputedStyle(el)
    const range = document.createRange(), walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
    let text: Node | null
    while ((text = walker.nextNode())) { if (text.textContent?.trim()) break }
    range.selectNodeContents(text!)
    return { fontSize: style.fontSize, lineHeight: style.lineHeight, textTop: range.getBoundingClientRect().top }
  }))
  expect(typography[0]).toEqual(typography[1])
  expect(typography[1]).toEqual(typography[2])
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
  await expect(dialog.getByText('酷狗暂时限流', { exact: false })).toHaveCount(0)
  await expect(dialog.getByLabel('歌词来源进度')).toHaveCount(0)
  await duration.fill('1:23')
  await dialog.getByRole('button', { name: '重试 酷狗音乐', exact: true }).click()
  expect(await page.evaluate(() => (window as any).__submittedLyricQuery.durationMs)).toBe(299123)
  await expect(qq).toHaveAttribute('aria-pressed', 'true')
  await expect(dialog.getByLabel('可读歌词')).toContainText('Hello world')
  expect(await page.evaluate(() => (window as any).__lyricReads)).toBe(1)
  const left = (await dialog.getByLabel('歌词候选').boundingBox())!, right = (await dialog.getByLabel('歌词预览', { exact: true }).boundingBox())!
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

test('XLRC voices, ruby and multiple languages survive preview and adoption', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string) => source === 'qq' ? [{ id: 'xlrc', source, sortKey: 'xlrc', title: 'XLRC 示例', artist: 'Artist', album: '', durationMs: 10000, matchScore: 100, totalScore: 130, highConfidence: false, autoPreviewEligible: false, matchNotes: [], matchTerms: [], scoreTerms: [] }] : []
    w.__lyricDocument = () => ({ text: '[xlrc:0.1]\n[x-sonexa:1]\n[lang:ja]\n[00:01.000][v:A]私[わたし]\n[>en]I\n[>fr]Moi\n[>ja-Latn]watashi\n[00:01.000][v:B]君[きみ]\n[>en]You', format: 'xlrc', offsetMs: 0, rows: [
      { timeMs: 1000, channel: 'original', text: '私', voice: 'A', ruby: [{ start: 0, end: 1, reading: 'わたし' }], translation: ['I', 'Moi'], romanization: ['watashi'], annotations: [{ channel: 'translation', language: 'en', text: 'I' }, { channel: 'translation', language: 'fr', text: 'Moi' }, { channel: 'romanization', language: 'ja-Latn', text: 'watashi' }] },
      { timeMs: 1000, channel: 'original', text: '君', voice: 'B', ruby: [{ start: 0, end: 1, reading: 'きみ' }], translation: ['You'], annotations: [{ channel: 'translation', language: 'en', text: 'You' }] },
    ], translation: { status: 'available', origin: 'independent', syncStatus: 'synced' }, romanization: { status: 'available', origin: 'independent', syncStatus: 'synced' } })
    const adopt = api.AdoptLyrics
    api.AdoptLyrics = async (...args: any[]) => { const result = await adopt(...args); w.__lyricText = w.__lyricDocument().text; return result }
  })
  await page.getByLabel('歌曲详情抽屉').getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await expect(dialog.getByText('酷我音乐', { exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await dialog.getByRole('button', { name: /XLRC 示例/ }).click()
  await expect(dialog.getByLabel('人声 A')).toBeVisible(); await expect(dialog.getByLabel('人声 B')).toBeVisible()
  await expect(dialog.getByLabel('可读歌词').locator('rt')).toHaveText(['わたし', 'きみ'])
  await expect(dialog.getByLabel('可读歌词')).toContainText('frMoi')
  await dialog.getByRole('button', { name: '原始文本', exact: true }).click()
  await expect(dialog.getByLabel('原始歌词文本')).toContainText('[>ja-Latn]watashi')
  await dialog.getByRole('button', { name: '应用到歌曲' }).click()
  expect(await page.evaluate(() => (window as any).__lyricText)).toContain('[v:B]君[きみ]')
})

test('extra channel selector switches only the third column and preserves every channel for adoption', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string) => source === 'qq' ? ['bilingual', 'plain'].map(id => ({ id, source, sortKey: id, title: `候选-${id}`, artist: 'Artist', album: 'Album', durationMs: 90000, matchScore: 80, totalScore: 130, highConfidence: false, autoPreviewEligible: false, matchNotes: [], matchTerms: [], scoreTerms: [] })) : []
    w.__lyricDocument = (id: string) => id === 'plain'
      ? { text: '纯文本原文', format: 'text', offsetMs: 0, rows: [{ text: '纯文本原文', channel: 'original' }] }
      : { text: '[offset:100]\n[00:01.000]<00:01.000>Hello <00:01.500>world\n[00:01.000]译文首行\n[00:01.000]译文续行\n[00:01.000]hello roma\n[00:02.000]独立译文\n[00:03.000]solo roma\n未同步译文\nunsynced roma\n未分类谐音', format: 'enhanced_lrc', offsetMs: 100,
          rows: [{ timeMs: 1000, text: 'Hello world', channel: 'original', translation: ['译文首行', '译文续行'], romanization: ['hello roma'] }, { timeMs: 2000, text: '独立译文', channel: 'translation' }, { timeMs: 3000, text: 'solo roma', channel: 'romanization' }, { text: '未同步译文', channel: 'translation' }, { text: 'unsynced roma', channel: 'romanization' }, { text: '未分类谐音', channel: 'additional' }, { text: '长行'.repeat(200), channel: 'original', translation: ['翻译'.repeat(200)], romanization: ['roma'.repeat(200)] }],
          translation: { status: 'available', origin: 'independent', syncStatus: 'partial' }, romanization: { status: 'available', origin: 'independent', syncStatus: 'partial' } }
    w.__previewAcks = 0
    w.__lyricAcknowledge = async () => { w.__previewAcks++ }
    const adopt = api.AdoptLyrics
    api.AdoptLyrics = async (session: string, id: string, revision: number) => {
      await adopt(session, id, revision); w.__adoptedText = w.__lyricDocument(id).text; return 1
    }
  })
  const open = async () => {
    await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  }
  await open()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' }), reading = dialog.getByLabel('可读歌词')
  const selector = dialog.getByRole('combobox', { name: '额外展示' })
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await dialog.getByRole('button', { name: /候选-bilingual/ }).click()
  await expect(selector).toHaveText('翻译')
  const toolbar = dialog.getByLabel('歌词预览工具栏')
  await expect(toolbar.getByRole('combobox', { name: '额外展示' })).toBeVisible()
  const modeBounds = (await toolbar.getByRole('group', { name: '预览模式' }).boundingBox())!
  const selectorBounds = (await selector.boundingBox())!
  expect(selectorBounds.x).toBeGreaterThan(modeBounds.x + modeBounds.width)
  expect(Math.abs(selectorBounds.y + selectorBounds.height / 2 - modeBounds.y - modeBounds.height / 2)).toBeLessThan(1)
  expect(selectorBounds.y).toBeGreaterThan((await dialog.getByLabel('歌词预览', { exact: true }).boundingBox())!.y)
  await expect(reading).toContainText('译文首行')
  await expect(reading).not.toContainText('hello roma')
  await expect(reading.getByRole('row').filter({ hasText: '独立译文' }).getByRole('cell')).toHaveText(['00:02.000', '', '翻译 · 独立行独立译文'])
  await expect(reading.getByRole('row').filter({ hasText: '未同步译文' }).getByRole('cell').first()).toHaveText('—')
  await expect(dialog.getByText('全局偏移：100 毫秒', { exact: false })).toBeVisible()
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeEnabled()
  // Keyboard interaction with the Volt Select must not reset reading/approval.
  await selector.focus(); await page.keyboard.press('ArrowDown')
  await expect(selector).toHaveAttribute('aria-expanded', 'true')
  await expect(page.getByRole('option')).toHaveText(['翻译', '罗马音'])
  await page.keyboard.press('End')
  await expect(selector).toHaveAttribute('aria-activedescendant', (await page.getByRole('option', { name: '罗马音', exact: true }).getAttribute('id'))!)
  await page.keyboard.press('Enter')
  await expect(selector).toHaveText('罗马音')
  await expect(reading.getByRole('columnheader')).toHaveText(['时间', '歌词', '罗马音'])
  await expect(reading).toContainText('Hello world')
  await expect(reading).toContainText('hello roma')
  await expect(reading).not.toContainText('译文首行')
  await expect(reading).not.toContainText('独立译文')
  await expect(reading.getByRole('row').filter({ hasText: 'solo roma' }).getByRole('cell')).toHaveText(['00:03.000', '', '罗马音 · 独立行solo roma'])
  await expect(reading.getByRole('row').filter({ hasText: 'unsynced roma' }).getByRole('cell').first()).toHaveText('—')
  await expect(reading).toContainText('附加文本（归属未确认）')
  await expect(reading).toContainText('未分类谐音')
  expect(await dialog.getByLabel('歌词预览', { exact: true }).evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('lyrics-three-columns.png') })
  await dialog.getByRole('button', { name: '原始文本', exact: true }).click()
  const raw = dialog.getByLabel('原始歌词文本'), text = await page.evaluate(() => (window as any).__lyricDocument('bilingual').text)
  await expect(raw).toHaveText(text)
  await selector.click(); await page.getByRole('option', { name: '翻译', exact: true }).click()
  await expect(raw).toHaveText(text)
  await dialog.getByRole('button', { name: '阅读', exact: true }).click()
  await expect(reading).toContainText('译文首行')
  expect(await page.evaluate(() => [(window as any).__lyricReads, (window as any).__previewAcks])).toEqual([1, 1])
  await selector.click(); await page.getByRole('option', { name: '罗马音', exact: true }).click()
  await dialog.getByRole('button', { name: '应用到歌曲' }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__adoptedText)).toBe(text)
  await dialog.getByRole('button', { name: '关闭歌词提示' }).click()
  await dialog.getByRole('button', { name: /候选-plain/ }).click()
  await expect(selector).toHaveText('罗马音')
  await expect(reading.getByRole('row').nth(1).getByRole('cell')).toHaveText(['—', '纯文本原文', ''])
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await open()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await dialog.getByRole('button', { name: /候选-bilingual/ }).click()
  await expect(selector).toHaveText('翻译')
})

test('normalized precision-aligned rows share the original time in reading and adopted text', async ({ page }, testInfo) => {
  await page.evaluate(() => {
    const w = window as any, api = w.go.app.App
    w.__lyricSearch = async (source: string) => source === 'qq' ? [{ id: 'aligned', source, sortKey: '01', title: '精度对齐候选', matchScore: 80, totalScore: 130, highConfidence: false, autoPreviewEligible: false }] : []
    const diagnostic = { code: 'time_precision_alignment', message: '时间精度对齐', inference: { ruleVersion: 'time-precision-v1', layer: 1, count: 2, associations: [
      { sourcePosition: 0, originalPosition: 0, sourceTimeMs: 42740, associatedTimeMs: 42743 },
      { sourcePosition: 1, originalPosition: 1, sourceTimeMs: 47850, associatedTimeMs: 47856 },
    ] } }
    // Complete producer output; frontend must not reconstruct associations or
    // use the diagnostic's old times as the visible/serialized row time.
    w.__lyricDocument = () => ({ text: '[00:42.743]<00:42.743>First <00:43.100>line\n[00:42.743]第一句译文\n[00:42.743]first roma\n[00:47.856]<00:47.856>Second <00:48.100>line\n[00:47.856]第二句译文\n[00:47.856]second roma', format: 'enhanced_lrc', offsetMs: 0, wordTiming: 'available',
      rows: [{ timeMs: 42743, text: 'First line', channel: 'original', translation: ['第一句译文'], romanization: ['first roma'] }, { timeMs: 47856, text: 'Second line', channel: 'original', translation: ['第二句译文'], romanization: ['second roma'] }],
      translation: { status: 'available', origin: 'independent', syncStatus: 'synced', diagnostics: [diagnostic] }, romanization: { status: 'available', origin: 'independent', syncStatus: 'synced', diagnostics: [diagnostic] } })
    const adopt = api.AdoptLyrics
    api.AdoptLyrics = async (session: string, id: string, revision: number) => { await adopt(session, id, revision); w.__alignedSaved = w.__lyricDocument().text; return 1 }
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' }), reading = dialog.getByLabel('可读歌词')
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  const card = dialog.getByRole('button', { name: /精度对齐候选/ })
  await expect(card).toContainText('翻译：有（时间精度对齐）')
  await expect(card).not.toContainText('启发式识别')
  await card.click()
  await expect(reading.getByRole('row')).toHaveCount(3)
  await expect(reading.getByRole('row').nth(1).getByRole('cell')).toHaveText(['00:42.743', 'First line', '第一句译文'])
  await expect(reading.getByRole('row').nth(2).getByRole('cell')).toHaveText(['00:47.856', 'Second line', '第二句译文'])
  await expect(reading).not.toContainText('独立行')
  await page.screenshot({ path: testInfo.outputPath('lyrics-precision-aligned.png') })
  await dialog.getByRole('combobox', { name: '额外展示' }).click()
  await page.getByRole('option', { name: '罗马音', exact: true }).click()
  await expect(reading.getByRole('row').nth(1).getByRole('cell')).toHaveText(['00:42.743', 'First line', 'first roma'])
  await dialog.getByRole('button', { name: '原始文本', exact: true }).click()
  const raw = dialog.getByLabel('原始歌词文本')
  await expect(raw).toContainText('[00:42.743]第一句译文')
  await expect(raw).toContainText('[00:47.856]second roma')
  await expect(raw).not.toContainText('[00:42.740]')
  await expect(raw).not.toContainText('[00:47.850]')
  await dialog.getByRole('button', { name: '应用到歌曲' }).click()
  await expect.poll(() => page.evaluate(() => (window as any).__alignedSaved)).toBe(await raw.textContent())
  expect(await page.evaluate(() => (window as any).__lyricReads)).toBe(1)
})

test('failed searches and operations remain recoverable without raw errors or layout expansion', async ({ page }) => {
  await page.evaluate(() => {
    const w = window as any
    w.__lyricSearch = async () => { throw new Error('RAW_SOURCE_ERROR /private/internal') }
  })
  await page.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(dialog.getByLabel('歌词候选')).toContainText('本次搜索未完成，请重试或更换来源。')
  await expect(dialog).not.toContainText('RAW_SOURCE_ERROR')
  await expect(dialog).not.toContainText('无匹配歌曲')
  await page.evaluate(() => {
    const w = window as any
    w.__lyricSearch = async (source: string) => [{ id: 'recovered', source, sortKey: '01', title: '重试成功候选', matchScore: 80, highConfidence: false, autoPreviewEligible: false }]
    w.go.app.App.FetchLyrics = async () => { throw new Error('RAW_FETCH_ERROR') }
  })
  await dialog.getByRole('button', { name: '重试 QQ 音乐', exact: true }).click()
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：共 1 个候选', exact: true })).toHaveText('1')
  await expect(dialog.getByRole('status', { name: '网易云音乐：获取异常，可重试', exact: true }).locator('svg')).toBeVisible()
  await dialog.getByRole('button', { name: /重试成功候选/ }).click()
  await expect(dialog.getByLabel('歌词预览', { exact: true })).toContainText('歌词暂不可用，请重试或选择其他候选。')
  await expect(dialog).not.toContainText('RAW_FETCH_ERROR')
  await expect(dialog.getByRole('button', { name: '应用到歌曲' })).toBeDisabled()
  const before = (await dialog.getByLabel('歌词预览', { exact: true }).boundingBox())!
  await dialog.getByRole('textbox', { name: '歌曲', exact: true }).fill('')
  await dialog.getByRole('textbox', { name: '艺术家', exact: true }).fill('')
  await dialog.getByRole('textbox', { name: '专辑（辅助匹配）', exact: true }).fill('')
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect(dialog.getByRole('alert')).toHaveText('请输入歌词搜索条件')
  expect((await dialog.getByLabel('歌词预览', { exact: true }).boundingBox())!).toEqual(before)
  await dialog.getByRole('button', { name: '关闭歌词提示' }).click()
  await expect(dialog.getByRole('alert')).toHaveCount(0)
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
  await expect(dialog.getByRole('status', { name: '网易云音乐：共 4 个候选', exact: true })).toHaveText('4')
  await expect(dialog.getByRole('status', { name: '酷狗音乐：共 0 个候选', exact: true })).toHaveText('0')
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：获取异常，共 1 个候选，可重试', exact: true }).locator('svg')).toBeVisible()
  await expect(list.locator('[data-candidate-id="pending"]').getByRole('button', { name: '综合得分 —/150', exact: true })).toHaveText('—/150')
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
  expect(await dialog.getByLabel('歌词预览', { exact: true }).evaluate(el => el.scrollWidth <= el.clientWidth + 1)).toBe(true)
  await page.evaluate(() => {
    const w = window as any, event = structuredClone(w.__lyricProgress.qq)
    event.sequence = 9; event.candidates[0].translation.status = 'available'; event.candidates[0].candidate.totalScore = 0
    w.runtime.EventsEmit('lyrics:progress', event)
  })
  await expect(list.locator('[data-candidate-id="b"]')).toContainText('翻译：失败')
  await expect(list.locator('[data-candidate-id="b"]').getByRole('button', { name: '综合得分 150/150', exact: true })).toHaveText('150/150')
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
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：共 1 个候选', exact: true })).toHaveText('1')
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：正在预取', exact: true })).toBeVisible()
  await page.evaluate(() => {
    const w = window as any, event = structuredClone(w.__lyricProgress.qq)
    event.sequence++; event.phase = 'stopped'; event.candidates[0].fetchState = 'skipped'
    w.runtime.EventsEmit('lyrics:progress', event)
  })
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：获取异常，共 1 个候选，可重试', exact: true }).locator('svg')).toBeVisible()
  await expect(dialog.getByRole('button', { name: '重试 QQ 音乐', exact: true })).toBeEnabled()
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
  for (const name of ['网易云音乐', '酷狗音乐']) await dialog.getByText(name, { exact: true }).click()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  await expect.poll(() => page.evaluate(() => typeof (window as any).__finishLyricsStart)).toBe('function')
  await expect(dialog.getByRole('status', { name: 'QQ 音乐：正在搜索', exact: true }).filter({ hasText: '—' })).toHaveText('—')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await page.evaluate(() => (window as any).__finishLyricsStart())
  await expect.poll(() => page.evaluate(() => (window as any).__cancelledLyrics)).toContain('late-session')
  await expect(dialog).toHaveCount(0)
})
