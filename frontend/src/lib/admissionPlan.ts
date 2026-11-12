import type { Models } from '@/api'

/**
 * 入库确认弹窗的判定逻辑。弹窗与 store 只消费这里的结论，规则只写一处。
 *
 * 后端的准入基线已经算好了全部事实（覆盖结论、提供者、不可判定的发行、判组结果），
 * 这里只把它们翻译成「弹窗处于哪种情形、有哪些动作」。任何闸门都在服务端：
 * 前端的输入校验只用于启用按钮。
 */
export type Plan = Models.app.AdmissionPlanView
export type Baseline = Models.domain.AdmissionBaseline
export type SongProvider = Models.domain.SongProvider

/** 允许同一首歌进入另一个发行时必须输入的确认词；服务端按同一规则校验。 */
export const TYPED_CONFIRM_WORD = 'Yes'

/** 输入是否等于确认词：去首尾空白，区分大小写。 */
export function typedWordMatches(input: string): boolean {
  return input.trim() === TYPED_CONFIRM_WORD
}

export type AdmissionMode =
  /** 归组证据命中多个组，必须先选判组动作。 */
  | 'conflict'
  /** 组内已有发行能提供（或疑似能提供）这首歌。 */
  | 'provided'
  /** 组内有发行清单不完整，无法确认它是否含这首歌。 */
  | 'unknown'
  /** 普通的发行成员变化确认。 */
  | 'membership'
  /** 不需要任何确认，可直接提交。 */
  | 'none'

export function admissionMode(baseline: Baseline | null | undefined): AdmissionMode {
  if (!baseline) return 'none'
  if (baseline.conflict) return 'conflict'
  if (baseline.needsTypedConfirm) return baseline.coverage === 'unknown' ? 'unknown' : 'provided'
  return baseline.requiresConfirmation ? 'membership' : 'none'
}

/** 计划能否不经弹窗直接提交。 */
export function commitsDirectly(baseline: Baseline | null | undefined): boolean {
  return admissionMode(baseline) === 'none'
}

export function admissionHeader(mode: AdmissionMode): string {
  switch (mode) {
    case 'conflict': return '归组证据冲突'
    case 'provided': return '这首歌可由已有发行提供'
    case 'unknown': return '无法确认已有发行是否包含这首歌'
    default: return '确认发行成员变化'
  }
}

export function admissionIntro(mode: AdmissionMode): string {
  switch (mode) {
    case 'conflict': return '证据命中多个专辑组。请选择判组方式，检查成员变化后再确认；也可取消并修正资料。'
    case 'provided': return '同一专辑组里已有发行包含这首歌。默认使用已有发行；仍要让它进入另一个发行，需要输入确认词。'
    case 'unknown': return '专辑组里有发行的曲目清单还不完整，无法判断它是否已经包含这首歌。可以先获取清单；仍要继续则需要输入确认词。'
    default: return '本次将新增发行、身份映射或合并专辑组。确认前不会改变曲库或复核条目。'
  }
}

export interface ProviderRelease {
  releaseId: number
  title: string
  credit: string
  source: string
  ref: string
  /** 所在发行的曲目引用数与清单总曲数，即主发行排序的依据。 */
  trackRefs: number
  trackTotal: number
  entries: SongProvider[]
  /** 能否直接改用：上游清单条目要有曲目标识，本地发行要有可复用的曲目。 */
  usable: boolean
}

/** 按发行归拢提供者条目；顺序沿用后端给出的顺序，主发行在最前。 */
export function providerReleases(baseline: Baseline | null | undefined): ProviderRelease[] {
  const out: ProviderRelease[] = []
  for (const p of baseline?.providers ?? []) {
    let release = out.find(r => r.releaseId === p.releaseId)
    if (!release) {
      release = {
        releaseId: p.releaseId, title: p.title || '未命名发行', credit: p.credit || '', source: p.identity?.provider || '本地',
        ref: p.identity?.ref || '', trackRefs: p.trackRefs ?? 0, trackTotal: p.trackTotal ?? 0, entries: [], usable: false,
      }
      out.push(release)
    }
    release.entries.push(p)
    if (p.identity ? !!p.trackRef : !!p.trackId) release.usable = true
  }
  return out
}

export function evidenceLabel(evidence: string): string {
  return evidence === 'recording' ? '录音相同' : '疑似：曲名与时长相同'
}

export function formatDuration(ms: number | null | undefined): string {
  if (!ms || ms <= 0) return ''
  const seconds = Math.round(ms / 1000)
  return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
}

/** 提供者条目在清单里的位置；本地曲目没有位置时为空。 */
export function providerPosition(p: SongProvider): string {
  return p.discNumber && p.trackNumber ? `${p.discNumber}-${p.trackNumber}` : ''
}

export function undecidableReason(reason: string): string {
  return reason === 'tracklist_incomplete' ? '曲目清单尚未取全' : reason
}

/**
 * 被否决的名称证据的说明。来源名取自这次拟加入发行的身份，不在界面里写死任何上游。
 */
export function rejectedText(evidence: Models.domain.GroupEvidence, source: string | undefined): string {
  if (evidence.reason === 'native_mismatch') return `因 ${source || '上游'} 的发行组不同，未并入专辑组 #${evidence.groupId}`
  if (evidence.reason === 'separated') return `此前已判定不是同一组，未并入专辑组 #${evidence.groupId}`
  return `未并入专辑组 #${evidence.groupId}`
}

export interface GroupActions {
  /** 命中多个组：可合并到某组，或加入某组但不合并其余。 */
  choose: boolean
  /** 可保持独立成组：发行尚未入库、不是整发行换组，且确实命中了已有的组。 */
  separate: boolean
}

export function groupActions(plan: Plan | null | undefined): GroupActions {
  const baseline = plan?.baseline
  const groups = baseline?.resolution?.groups?.length ?? 0
  if (!plan || !baseline || !groups) return { choose: false, separate: false }
  const moving = !!plan.seed.moveReleaseId
  const named = (baseline.resolution.evidence ?? []).some(e => e.kind !== 'native' && e.kind !== 'existing-release')
  return {
    choose: !!baseline.conflict,
    separate: !moving && !baseline.existingReleaseId && !plan.seed.keepSeparate && (!!baseline.conflict || (named && !!baseline.requiresConfirmation)),
  }
}

/** 复核队列里各原因的说明。 */
export const REVIEW_REASONS: Record<string, string> = {
  new_release: '新增发行加入已有专辑组，接受后还需确认成员变化',
  song_unresolved: '无法确认已有发行是否包含这首歌，需复核',
  group_insufficient: '判组资料不足，可按已知信息保存',
  group_conflict: '专辑组证据冲突，需要明确判组方式',
  unknown_total: '发行总曲数未知，需要人工选版',
  no_match: '没有高置信匹配',
  ambiguous: '候选存在歧义',
  duplicate: '可能与已有收藏重复',
}

export function reviewReasonText(reason: string | null | undefined): string {
  return REVIEW_REASONS[reason ?? ''] ?? reason ?? ''
}
