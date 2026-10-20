export interface RankedLyrics {
  source: string
  sortKey: string
  matchScore: number
  totalScore?: number | null
  highConfidence: boolean
}

// SortKey is backend-produced ASCII hex, never a locale string or session ID.
export function compareLyrics(a: RankedLyrics, b: RankedLyrics): number {
  const aScored = a.totalScore != null
  const bScored = b.totalScore != null
  if (aScored !== bScored) return aScored ? -1 : 1
  if (aScored && bScored && a.totalScore !== b.totalScore) return b.totalScore! - a.totalScore!
  if (a.matchScore !== b.matchScore) return b.matchScore - a.matchScore
  if (a.highConfidence !== b.highConfidence) return a.highConfidence ? -1 : 1
  if (a.source !== b.source) return a.source < b.source ? -1 : 1
  if (a.sortKey !== b.sortKey) return a.sortKey < b.sortKey ? -1 : 1
  return 0
}
