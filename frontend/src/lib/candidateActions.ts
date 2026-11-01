import type { Models } from '@/api'
import { app as WailsApp } from '../../wailsjs/go/models'

type Row = Pick<Models.app.CandidateRow, 'action' | 'ownership'>

const labels: Record<string, string> = {
  accept: '接受', skip: '跳过', add: '加入曲库', owned: '已在库中', unavailable: '不可操作',
}

export function candidateActionLabel(row: Row): string {
  return labels[row.action] ?? '不可操作'
}

/** 动作与所有权须一致；相关 ID、徽章和展示文案从不授予操作权限。 */
export function candidateActionEnabled(row: Row): boolean {
  const state = row.ownership?.state
  if (row.action === 'add' || row.action === 'accept') return state === 'unowned'
  if (row.action === 'owned' || row.action === 'skip') return state === 'owned' && row.ownership.trackId > 0
  return false
}

export function invalidateCandidateOwnership(rows: Models.app.CandidateRow[], reason: string): Models.app.CandidateRow[] {
  return rows.map(row => new WailsApp.CandidateRow({
    ...row, action: 'unavailable', localTrackId: 0, badges: [row.provider], note: reason,
    ownership: { ...row.ownership, state: 'lookup_error', trackId: 0, revision: 0, releaseRevision: 0, reason },
    releaseId: 0, coverRevision: 0, coverError: reason,
  }))
}

export function candidateLocalTarget(row: Row): number {
  return row.ownership?.state === 'owned' ? row.ownership.trackId : 0
}
