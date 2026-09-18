/** 后端署名区间是 UTF-8 字节偏移，不能直接作为 JS slice 的 UTF-16 下标。 */
export interface CreditSpan {
  start: number
  end: number
  artistId: number
  text?: string
}
export interface CreditPart { text: string; artistId?: number }

export function creditParts(text: string, spans: CreditSpan[]): CreditPart[] {
  const offsets = new Map<number, number>([[0, 0]])
  const encoder = new TextEncoder()
  let byte = 0, char = 0
  for (const point of text) {
    byte += encoder.encode(point).length
    char += point.length
    offsets.set(byte, char)
  }
  const parts: CreditPart[] = []
  let cursor = 0
  for (const span of [...spans].sort((a, b) => a.start - b.start)) {
    const start = offsets.get(span.start), end = offsets.get(span.end)
    if (start === undefined || end === undefined || start < cursor || end <= start || span.artistId <= 0) continue
    const fragment = text.slice(start, end)
    if (span.text !== undefined && span.text !== fragment) continue // 旧署名的区间不误链新文本
    if (start > cursor) parts.push({ text: text.slice(cursor, start) })
    parts.push({ text: fragment, artistId: span.artistId })
    cursor = end
  }
  if (cursor < text.length) parts.push({ text: text.slice(cursor) })
  return parts
}
