// 后端推送的事件载荷类型。
//
// 这些类型不出现在任何绑定方法的签名中，因此不在生成的 models.ts 里，
// 需要在前端单独声明并与 Go 侧的结构保持一致。

import type { Models } from '@/api'

/** Data-only shapes derived from bindings, without generated class helpers. */
export type ProblemStats = Pick<Models.store.ProblemStats, 'reviewProblems' | 'pendingProblems' | 'failedProblems' | 'sourceRows'>
export type BatchStats = Pick<Models.store.BatchStats,
  'total' | 'queued' | 'matching' | 'imported' | 'review' | 'failed' | 'skipped' | 'discarded' | 'unmatched' | 'existingSkipped'
> & { problems: ProblemStats }

/** A committed task/problem transaction; not a library mutation. */
export interface TaskChange { revision: number }

export const emptyProblems: ProblemStats = {
  reviewProblems: 0, pendingProblems: 0, failedProblems: 0, sourceRows: 0,
}

/** 匹配进度事件。对应 importer.Progress。 */
export interface ImportProgress {
  batchId: number
  processed: number
  total: number
  stats: BatchStats
  /** 本次处理产生的日志行（各分流分支的提示文案）。 */
  line: string
  done: boolean
  paused: boolean
}

/** 侧栏任务区的状态。前端自行构造，故用本地接口而非生成的类。 */
export interface TaskState {
  activeBatch: number
  filename: string
  processed: number
  total: number
  stats: BatchStats | null
  running: boolean
  reviewPending: number
  problems: ProblemStats
}

export const emptyStats: BatchStats = {
  total: 0,
  queued: 0,
  matching: 0,
  imported: 0,
  review: 0,
  failed: 0,
  skipped: 0,
  discarded: 0,
  unmatched: 0,
  existingSkipped: 0,
  problems: { ...emptyProblems },
}
