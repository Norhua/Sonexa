import type { Models } from '@/api'

// Model shapes come from Wails; the bridge sends data, not class helper methods.
export type LyricData<T> = T extends (infer U)[] ? LyricData<U>[] : T extends object
  ? { [K in keyof T as T[K] extends (...args: never[]) => unknown ? never : K]: LyricData<T[K]> }
  : T
export type LyricDocument = LyricData<Models.lyricdoc.Document>
export type LyricExtraChannel = 'translation' | 'romanization'
export type LyricReadingRow = { timeMs?: number; text: string; extra: string[]; note: string; voice?: string; ruby?: LyricData<Models.lyricdoc.Ruby>[]; annotations?: LyricData<Models.lyricdoc.Annotation>[] }
export type LyricCandidate = Omit<LyricData<Models.lyrics.Candidate>, 'totalScore'> & { totalScore?: number | null }
export type LyricChannel = LyricData<Models.lyrics.ChannelSummary>
export type LyricCard = LyricCandidate & Omit<LyricData<Models.lyrics.CandidateSummary>, 'candidate'>
export type LyricProgress = Omit<LyricData<Models.app.LyricsProgress>, 'candidates'> & {
  candidates: (Omit<LyricData<Models.lyrics.CandidateSummary>, 'candidate'> & { candidate: LyricCandidate })[]
}

export const lyricFormat = (format?: string) => ({ xlrc: 'XLRC', text: '文本', lrc: 'LRC 时间轴歌词', enhanced_lrc: '增强型 LRC' }[format ?? ''] ?? '待确认')
export const lyricPhase = (phase: string) => ({ searching: '正在搜索', ready: '等待预取', prefetching: '正在预取', done: '已完成', partial: '部分完成', stopped: '已停止', error: '搜索失败' }[phase] ?? phase)
export const lyricFailure = (kind?: string) => ({ no_lyrics: '暂无歌词', format: '解析失败', contract: '文档校验失败', timeout: '已超时', cancelled: '已取消', budget: '请求预算耗尽', memory_limit: '会话内存已达上限', too_large: '歌词超限', auth: '来源需要认证', rate_limit: '来源限流', tls: '安全连接失败', transport: '网络请求失败', upstream: '来源限制', source_stopped: '来源已停止', fetch: '获取失败' }[kind ?? ''] ?? '尚未检查')
export const lyricChannelStatus = (channel?: LyricChannel) => {
  const status = channel?.status ?? 'pending'
  const label = ({ available: '有', unavailable: '未提供', pending: '待检查', loading: '获取中', error: '失败', unknown: '无法确认' }[status] ?? '无法确认')
  if (status !== 'available') return label
  const notes: string[] = []
  if (channel?.diagnostics?.some(d => d.ruleVersion && d.code !== 'time_precision_alignment')) notes.push('启发式识别')
  if (channel?.diagnostics?.some(d => d.code === 'time_precision_alignment')) notes.push('时间精度对齐')
  if (channel?.syncStatus === 'unsynced') notes.push('未同步')
  if (channel?.syncStatus === 'partial') notes.push('部分')
  return label + (notes.length ? `（${notes.join('、')}）` : '')
}
export const lyricChannelBasis = (channel?: LyricChannel) => channel?.diagnostics?.filter(d => d.ruleVersion)
  .map(d => d.code === 'time_precision_alignment'
    ? `规则 ${d.ruleVersion} · ${d.count} 行；±10 毫秒内唯一且无交叉地对齐原文行首，保留原时间与词时间`
    : `规则 ${d.ruleVersion} · 第 ${d.layer} 层 · ${d.count} 行；经验推断，可能误判`).join('；') ?? ''
export const lyricRoundFinished = (phase: string) => ['done', 'partial', 'stopped', 'error'].includes(phase)
export const lyricTime = (ms?: number) => ms == null ? '—' : `${String(Math.floor(ms / 60000)).padStart(2, '0')}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}.${String(ms % 1000).padStart(3, '0')}`

// Display only: preserve the provider's row associations and timestamps. Never
// pair independent extras by index or change the document used for adoption.
// Empty independent extras are timing markers, not text needing a row label.
export function lyricReadingRows(rows: LyricDocument['rows'], channel: LyricExtraChannel): LyricReadingRow[] {
  return rows.filter(row => row.channel === 'original' || (row.channel !== (channel === 'translation' ? 'romanization' : 'translation') && row.text.trim() !== '')).map(row => {
    if (row.channel === 'original') return { timeMs: row.timeMs, text: row.text, extra: row[channel] ?? [], note: '', voice: row.voice, ruby: row.ruby, annotations: row.annotations?.filter(a => a.channel === channel || a.channel === 'additional') }
    const name = ({ translation: '翻译', romanization: '罗马音' }[row.channel] ?? '附加文本（归属未确认）')
    return { timeMs: row.timeMs, text: '', extra: [row.text], note: `${name} · ${row.timeMs == null ? '未同步' : '独立行'}`, voice: row.voice,
      ...(row.language ? { annotations: [{ channel: row.channel, language: row.language, text: row.text, ruby: row.ruby }] } : {}) }
  })
}

// Go offsets are Unicode code points, so never slice UTF-16 strings directly.
export function lyricRubySegments(text: string, ruby: LyricData<Models.lyricdoc.Ruby>[] = []) {
  const chars = Array.from(text), segments: { text: string; reading?: string }[] = []
  let cursor = 0
  for (const annotation of ruby) {
    if (annotation.start < cursor || annotation.end <= annotation.start || annotation.end > chars.length) continue
    if (annotation.start > cursor) segments.push({ text: chars.slice(cursor, annotation.start).join('') })
    segments.push({ text: chars.slice(annotation.start, annotation.end).join(''), reading: annotation.reading })
    cursor = annotation.end
  }
  if (cursor < chars.length) segments.push({ text: chars.slice(cursor).join('') })
  return segments
}

// The backend scoring formula has a fixed maximum: 100 + 10 + 20 + 15 + 5.
export const lyricMaxScore = 150
export function lyricScoreDescription(card: LyricCandidate) {
  const notes = card.matchNotes?.join('；') || '暂无匹配说明'
  const confidence = card.highConfidence ? '高置信' : '需确认'
  return `歌曲匹配 ${card.matchScore} 分・歌词综合 ${card.totalScore ?? '—'}，${notes}${notes.includes(confidence) ? '' : `；${confidence}`}（分数不是概率）`
}

// This badge describes song search only, not later fetch/channel outcomes.
export function lyricSourceStatus(result: { state: string; phase: string; cards: LyricCard[]; progress?: LyricProgress }) {
  const count = result.state === 'ok' ? result.progress?.searched ?? result.cards.length : undefined
  const failed = result.state === 'error'
  return { count, failed, label: failed ? '搜索失败，可重试' : count != null ? `共 ${count} 个候选` : '正在搜索' }
}

// Search input preserves milliseconds; blank means unknown, never a zero-length song.
export function lyricDurationInput(ms?: number | null): string {
  if (!ms || ms <= 0) return ''
  const fraction = ms % 1000 ? `.${String(ms % 1000).padStart(3, '0')}` : ''
  return `${Math.floor(ms / 60000)}:${String(Math.floor(ms / 1000) % 60).padStart(2, '0')}${fraction}`
}
export function parseLyricDuration(value: string): number | undefined {
  const text = value.trim()
  if (!text) return 0
  if (!/^(?:\d+:)?\d+(?:\.\d{1,3})?$/.test(text)) return undefined
  const parts = text.split(':')
  if (parts.length === 2 && !/^[0-5]\d(?:\.\d{1,3})?$/.test(parts[1]!)) return undefined
  const ms = Math.round((parts.length === 2 ? Number(parts[0]) * 60 + Number(parts[1]) : Number(parts[0])) * 1000)
  return Number.isSafeInteger(ms) && ms > 0 && ms <= 86400000 ? ms : undefined
}

export function pendingLyricCard(candidate: LyricCandidate, prefetch: boolean): LyricCard {
  const pending = (): LyricChannel => ({ status: 'pending', origin: 'unknown', syncStatus: 'unknown' })
  return { ...candidate, prefetch, fetchState: 'pending', documentVersion: 0, wordTiming: 'pending', translation: pending(), romanization: pending() }
}
