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
    api.SearchLyrics = async (_: string, source: string) => {
      if (source === 'kuwo') throw new Error('酷我暂时限流')
      return [{ id: source, source, title: `歌曲-${source}`, artist: '歌手', album: '专辑', durationMs: 123000 }]
    }
    api.FetchLyrics = async (_: string, id: string) => ({ text: id === 'qq' ? '[00:01.00]<00:01.00>网络<00:02.00>歌词' : '纯文本歌词', format: id === 'qq' ? 'enhanced_lrc' : 'text' })
    api.AdoptLyrics = async (session: string, id: string, revision: number) => {
      w.__lyricAdopt = { session, id, revision }; w.__lyricText = '[00:01.00]网络歌词'; w.runtime.EventsEmit('library:changed'); return 1
    }
  })
  const drawer = page.getByLabel('歌曲详情抽屉')
  await drawer.getByRole('button', { name: /查看歌词/ }).click({ button: 'right' })
  await page.getByRole('menuitem', { name: '从网络搜索歌词' }).click()
  const dialog = page.getByRole('dialog', { name: '从网络搜索歌词' })
  await expect(dialog.getByText('仅修改当前歌曲的歌词', { exact: false })).toBeVisible()
  await dialog.getByRole('button', { name: '搜索', exact: true }).click()
  const qq = dialog.getByRole('button', { name: /歌曲-qq/ })
  await qq.click()
  await expect(qq).toContainText('来源：QQ 音乐 · 时长：2:03')
  await expect(qq).toContainText('类型：增强型 LRC')
  await expect(dialog.getByLabel('歌词预览').locator('pre')).toContainText('<00:01.00>网络')
  await expect(dialog.getByText('酷我暂时限流', { exact: false })).toBeVisible()
  const left = (await dialog.getByLabel('歌词候选').boundingBox())!, right = (await dialog.getByLabel('歌词预览').boundingBox())!
  expect(left.x + left.width).toBeLessThanOrEqual(right.x + 1)
  await page.screenshot({ path: testInfo.outputPath('lyrics-search.png') })
  await dialog.getByRole('button', { name: '应用到歌曲' }).click()
  await expect(dialog.getByText('歌词已应用到当前歌曲')).toBeVisible()
  expect(await page.evaluate(() => (window as any).__lyricAdopt.id)).toBe('qq')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await expect(drawer.getByRole('button', { name: /查看歌词/ })).toHaveAttribute('data-has-lyrics', 'true')
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
