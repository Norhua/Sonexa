// Display-only projection of saved TXT/LRC. Never write this back as adoption
// text; native QRC/YRC/KRC are already normalized by their producer.
const tagLine = /^\[[a-zA-Z][a-zA-Z0-9_-]*:[^\]]*\]$/
const leadingTime = /^\[(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?\]/
const wordTime = /<(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?>/g
const validTime = (minutes: string, seconds: string, fraction = '') => {
  const ms = Number(minutes) * 60000 + Number(seconds) * 1000 + Number(fraction.padEnd(3, '0'))
  return Number.isSafeInteger(ms) && ms <= 86400000
}

export function lyricsText(raw: string | null | undefined): string {
  if (!raw) return ''
  const normalized = raw.replace(/\r\n?/g, '\n').replace(/^\uFEFF/, '')
  const xlrc = /^(?:\[xlrc:|\[>)/m.test(normalized)
  const escaped = /^\[x-sonexa:1\]$/m.test(normalized)
  const lines: string[] = []
  for (const line of normalized.split('\n')) {
    if (tagLine.test(line.trim())) continue
    let body = line, timed = false
    for (;;) {
      const match = leadingTime.exec(body)
      if (!match || !validTime(match[1]!, match[2]!, match[3])) break
      timed = true; body = body.slice(match[0].length)
    }
    // Timestamp-like text in an untimed line is ordinary text. Only leading
    // square tags are controls, whereas words are parsed within a timed line.
    if (xlrc) body = body.replace(/^\[>[A-Za-z0-9]+(?:-[A-Za-z0-9]+)*\]/, '').replace(/^\[x-text\]/, '').replace(/^\[x-role:[^\]]+\]/, '')
    if (timed || xlrc) {
      body = body.replace(/^\[v:[^\]]*\]/, '').replace(/^\[x-end:\d+\]/, '')
      body = body.replace(wordTime, (tag, m: string, s: string, f: string | undefined, offset: number, whole: string) => {
        let slashes = 0
        for (let i = offset - 1; i >= 0 && whole[i] === '\\'; i--) slashes++
        return escaped && slashes % 2 || !validTime(m, s, f) ? tag : ''
      })
      // Only contiguous Han + kana brackets are ruby; ordinary brackets stay.
      body = body.replace(/([\p{Script=Han}々〆ヵヶ]+)\[([\u3040-\u30ff\u31f0-\u31ff\uff66-\uff9f]+)\]/gu, '$1')
      if (escaped) body = body.replace(/\\([\\[<])/g, '$1')
    }
    lines.push(body)
  }
  return lines.join('\n')
}
