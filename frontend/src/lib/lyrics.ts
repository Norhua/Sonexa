// Display-only projection of saved TXT/LRC. Never write this back as adoption
// text; native QRC/YRC/KRC are already normalized by their producer.
const tagLine = /^\[[a-zA-Z]+:[^\]]*\]$/
const leadingTime = /^\[(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?\]/
const wordTime = /<(\d+):([0-5]?\d)(?:[.:](\d{1,3}))?>/g
const validTime = (minutes: string, seconds: string, fraction = '') => {
  const ms = Number(minutes) * 60000 + Number(seconds) * 1000 + Number(fraction.padEnd(3, '0'))
  return Number.isSafeInteger(ms) && ms <= 86400000
}

export function lyricsText(raw: string | null | undefined): string {
  if (!raw) return ''
  const lines: string[] = []
  for (const line of raw.replace(/\r\n?/g, '\n').replace(/^\uFEFF/, '').split('\n')) {
    if (tagLine.test(line.trim())) continue
    let body = line, timed = false
    for (;;) {
      const match = leadingTime.exec(body)
      if (!match || !validTime(match[1]!, match[2]!, match[3])) break
      timed = true; body = body.slice(match[0].length)
    }
    // Timestamp-like text in an untimed line is ordinary text. Only leading
    // square tags are controls, whereas words are parsed within a timed line.
    if (timed) body = body.replace(wordTime, (tag, m: string, s: string, f?: string) => validTime(m, s, f) ? '' : tag)
    lines.push(body)
  }
  return lines.join('\n')
}
