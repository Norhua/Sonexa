// LRC 的标签行（[ti:] [ar:] [kana:] 等）与时间标签只用于播放器，
// 详情里只展示歌词正文。
const tagLine = /^\[[a-zA-Z]+:[^\]]*\]$/
const timeTag = /\[\d{1,3}:\d{1,2}(?:[.:]\d{1,3})?\]/g

/** lyricsText 把 LRC 转为纯文本；不是 LRC 的原文原样保留。 */
export function lyricsText(raw: string | null | undefined): string {
  if (!raw) return ''
  const lines: string[] = []
  for (const line of raw.replace(/\r\n?/g, '\n').split('\n')) {
    const trimmed = line.trim()
    if (tagLine.test(trimmed)) continue
    lines.push(trimmed.replace(timeTag, '').trim())
  }
  return lines.join('\n').replace(/\n{3,}/g, '\n\n').trim()
}
