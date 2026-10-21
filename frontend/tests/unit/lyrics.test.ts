import { describe, expect, it } from 'vitest'
import { lyricsText } from '../../src/lib/lyrics'
import fixture from '../../../testdata/lyrics/text.json'

describe('lyricsText', () => {
  it('去掉 LRC 标签行与时间标签', () => {
    const raw = '[ti:晴天]\n[ar:周杰伦]\n[offset:0]\n[00:01.20]故事的小黄花\n[00:05.00][01:10.5]从出生那年就飘着\n'
    expect(lyricsText(raw)).toBe('故事的小黄花\n从出生那年就飘着\n')
  })
  it('增强型 LRC 隐藏逐字时间但保留正文符号', () => {
    expect(lyricsText('[00:01.00]<00:01.00>逐<00:02.00>字\n1 < 2')).toBe('逐字\n1 < 2')
  })
  it('共享语料保留正文、段落及标签边界，不展开重复时间戳', () => {
    const expected = [
      ' 1 < 2 (hello) [aside] <b>text</b> 😀 é　\n\nnext ',
      '副歌\n again ',
      'Hello world',
      ' A 😀 (B)',
      'literal [00:02] (10,20)\nplain <00:01> literal',
      '\n\n  hello\n\nworld  \n',
    ]
    fixture.forEach((entry, i) => {
      expect(lyricsText(entry.input), entry.name).toBe(expected[i])
      expect(lyricsText(entry.text), `${entry.name} saved`).toBe(expected[i])
    })
  })
  it('非法和超限标签不误删，正文中的方括号保持原样', () => {
    expect(lyricsText('[00:01]text [00:02] <00:99> <99999999:01>')).toBe('text [00:02] <00:99> <99999999:01>')
    expect(lyricsText('plain <00:01> [00:02]\n [00:01]not leading')).toBe('plain <00:01> [00:02]\n [00:01]not leading')
  })
  it('空值与非 LRC 文本', () => {
    expect(lyricsText(undefined)).toBe('')
    expect(lyricsText('纯文本歌词')).toBe('纯文本歌词')
  })
})
