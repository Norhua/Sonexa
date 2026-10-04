import type { Row } from './trackRows'

export type AlbumTrackRow = Row & { releaseId: number; provider?: string | null }
const normalized = (value?: string | null) => (value ?? '').normalize('NFKC').trim().replace(/\s+/g, ' ').toLocaleLowerCase()

function sameSong(a: AlbumTrackRow, b: AlbumTrackRow) {
  if (a.provider && a.provider === b.provider) {
    if (a.trackRef && a.trackRef === b.trackRef) return true
    if (a.recordingRef && b.recordingRef) return a.recordingRef === b.recordingRef
  }
  // 跨来源没有统一身份时只合并明确的标题、署名和相近时长，空值不能作证据。
  return !!normalized(a.title) && !!normalized(a.artist) &&
    normalized(a.title) === normalized(b.title) && normalized(a.artist) === normalized(b.artist) &&
    a.durationMs != null && b.durationMs != null && Math.abs(a.durationMs - b.durationMs) <= 3000
}

/** 只合并展示：同发行重复收录保持独立，所有收藏与灰色行的发行上下文完整保留。 */
export function mergeAlbumRows(rows: AlbumTrackRow[]): AlbumTrackRow[] {
  const groups: AlbumTrackRow[][] = []
  for (const row of rows) {
    const matches = groups.filter(group => !group.some(other => other.releaseId === row.releaseId) &&
      !group.some(other => other.provider && other.provider === row.provider && other.recordingRef && row.recordingRef && other.recordingRef !== row.recordingRef) &&
      group.some(other => sameSong(other, row)))
    if (matches.length === 1) matches[0]!.push(row)
    else groups.push([row])
  }
  return groups.map(group => {
    const owned = group.filter(row => !row.ghost)
    const display = owned[0] ?? group[0]!
    return { ...display, rowKey: `merged:${group[0]!.rowKey}`, groupMembers: owned.length > 1 ? owned : undefined }
  })
}
