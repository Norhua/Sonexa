/**
 * 取图失败成因的查询。
 *
 * 「上游没有这张封面」与「取图出错」对用户是两件事：前者该显示 nil，
 * 后者该显示失败图标。但 <img> 的 error 事件读不到状态码——两种失败在前端
 * 看起来一模一样，只能问后端。
 *
 * 查询只在 <img> 已经失败之后才发起，且**整批一起问**：一页列表里取不到图的行
 * 往往是一起失败的（同一批发行在 Cover Art Archive 上都没有图），
 * 逐行一次 IPC 会把几十次调用摊在同一帧里。
 */
import { App, type CoverSource } from '@/api'

/** 取图失败的两种成因。 */
export type CoverOutcome = 'empty' | 'error'

/** 攒批的时间窗。一帧之内失败的行合成一次调用。 */
const BATCH_MS = 30

interface Pending {
  provider: string
  ref: string
  size: number
  resolve: (o: CoverOutcome) => void
}

let pending: Pending[] = []
let timer: ReturnType<typeof setTimeout> | null = null

async function flush() {
  timer = null
  const batch = pending
  pending = []
  if (batch.length === 0) return

  try {
    const got = await App.CoverOutcomes(
      batch.map((p) => ({ provider: p.provider, ref: p.ref, size: p.size })),
    )
    batch.forEach((p, i) => p.resolve(got?.[i] === 'empty' ? 'empty' : 'error'))
  } catch {
    // 问不到成因时按「取图出错」呈现：这一次的确是失败了，
    // 只是说不清为什么——不能因此显示成「上游没有封面」。
    batch.forEach((p) => p.resolve('error'))
  }
}

/** 查询一张封面最近一次取图的失败成因。 */
export function coverOutcome(
  source: CoverSource | null | undefined,
  size: number,
): Promise<CoverOutcome> {
  if (!source?.provider || !source.ref) return Promise.resolve('error')
  const { provider, ref } = source
  return new Promise((resolve) => {
    pending.push({ provider, ref, size, resolve })
    if (timer === null) timer = setTimeout(flush, BATCH_MS)
  })
}
