import type { CoverSource } from '@/api'

/** 曲目身份与清单位置分开；灰色行没有库内 id。 */
export interface Row {
  id: number
  title: string
  artist?: string | null
  album?: string | null
  /** 首次加入资料库的 RFC3339 时间；灰色行没有入库时间。 */
  addedAt?: string | null
  year?: number | null
  trackNumber?: number | null
  discNumber?: number | null
  durationMs?: number | null
  genre?: string | null
  mediumFormat?: string | null
  country?: string | null
  coverSource?: CoverSource | null
  adoptedCoverPath?: string | null
  ghost?: boolean
  trackRef?: string | null
  recordingRef?: string | null
  /** 清单行由父级提供稳定位置；普通库内行不需要。 */
  rowKey?: string
}
export interface ListRow extends Row { key: string }

export function listRows(rows: Row[]): ListRow[] {
  return rows.map(row => ({
    ...row,
    key: row.rowKey ?? (row.ghost
      ? `ghost:${row.discNumber ?? 0}:${row.trackNumber ?? 0}:${row.trackRef ?? ''}`
      : `track:${row.id}`),
  }))
}

const collator = new Intl.Collator('zh-Hans-CN', { numeric: true, sensitivity: 'base' })
const fields: Record<string, keyof Row> = {
  title: 'title', artist: 'artist', album: 'album', year: 'year', duration: 'durationMs', addedAt: 'addedAt',
}
export function sortRows(rows: ListRow[], key: string, desc: boolean): ListRow[] {
  const field = fields[key]
  if (!field) return rows
  return [...rows].sort((a, b) => {
    const av = field === 'addedAt' ? timestamp(a.addedAt) : a[field]
    const bv = field === 'addedAt' ? timestamp(b.addedAt) : b[field]
    // 缺失值始终排在末尾，不把未知年份当成 0。
    if (av == null || av === '') return bv == null || bv === '' ? 0 : 1
    if (bv == null || bv === '') return -1
    const value = typeof av === 'number' && typeof bv === 'number'
      ? av - bv : collator.compare(String(av), String(bv))
    return (desc ? -1 : 1) * value
  })
}

function timestamp(value: string | null | undefined) {
  const time = value ? Date.parse(value) : NaN
  return Number.isFinite(time) ? time : null
}

/** 对外批量操作面向曲目而非清单位置，同一 id 只传一次。 */
export function uniqueTracks(rows: Row[]): Row[] {
  return [...new Map(rows.filter(row => !row.ghost && row.id > 0).map(row => [row.id, row])).values()]
}
