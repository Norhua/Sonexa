import { describe, expect, it } from 'vitest'
import { lyricsText } from '../../src/lib/lyrics'

describe('lyricsText', () => {
  it('去掉 LRC 标签行与时间标签', () => {
    const raw = '[ti:晴天]\n[ar:周杰伦]\n[offset:0]\n[00:01.20]故事的小黄花\n[00:05.00][01:10.5]从出生那年就飘着\n'
    expect(lyricsText(raw)).toBe('故事的小黄花\n从出生那年就飘着')
  })
  it('增强型 LRC 隐藏逐字时间但保留正文符号', () => {
    expect(lyricsText('[00:01.00]<00:01.00>逐<00:02.00>字\n1 < 2')).toBe('逐字\n1 < 2')
  })
  it('空值与非 LRC 文本', () => {
    expect(lyricsText(undefined)).toBe('')
    expect(lyricsText('纯文本歌词')).toBe('纯文本歌词')
  })
})
