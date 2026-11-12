import { defineStore } from 'pinia'
import { ref } from 'vue'
import { App, type Models } from '@/api'
import { admissionMode, commitsDirectly, typedWordMatches } from '@/lib/admissionPlan'

type Plan = Models.app.AdmissionPlanView
type Result = Models.domain.AdmissionResult
/** 所有交互入库共用同一计划确认；取消返回 null，来源页面保持原选择。 */
export const useAdmissionStore = defineStore('admission', () => {
  const plan = ref<Plan | null>(null)
  const busy = ref(false)
  const error = ref('')
  /** 正在为不可判定的发行获取清单；coverageError 是那次请求的失败原因。 */
  const fetching = ref(false)
  const coverageError = ref('')
  let prepare: (() => Promise<Plan>) | null = null
  let settle: ((result: Result | null) => void) | null = null
  let generation = 0
  let commitPlan = App.CommitAdmission
  const none = () => ({} as Models.domain.AdmissionApproval)
  function finish(result: Result | null) {
    generation++
    plan.value = null
    busy.value = false
    fetching.value = false
    coverageError.value = ''
    prepare = null
    const done = settle; settle = null; done?.(result)
  }
  async function run(next: () => Promise<Plan>, commit = App.CommitAdmission): Promise<Result | null> {
    if (busy.value || plan.value) throw new Error('请先完成当前入库确认')
    commitPlan = commit
    busy.value = true; error.value = ''; coverageError.value = ''
    try {
      const first = await next()
      if (commitsDirectly(first.baseline)) {
        try { return await commitPlan(first.id, first.hash, none()) }
        catch (e) { error.value = String(e) }
      }
      prepare = next
      plan.value = first
      const pending = new Promise<Result | null>(resolve => { settle = resolve })
      busy.value = false
      // 不可判定时自动获取一次清单再重新预览：多数情况下取到清单就不需要人工输入了。
      if (!error.value && admissionMode(first.baseline) === 'unknown') void refreshCoverage()
      return await pending
    } finally { busy.value = false }
  }
  async function refresh() {
    if (!prepare || busy.value) return
    const owner = ++generation
    busy.value = true; error.value = ''
    try { const next = await prepare(); if (owner === generation) plan.value = next }
    catch (e) { if (owner === generation) error.value = String(e) }
    finally { if (owner === generation) busy.value = false }
  }
  /** 在当前计划上执行一次服务端重新准备（判组动作、改用提供者、获取清单）。 */
  async function replan(next: (current: Plan) => Promise<Plan>, direct = false) {
    const current = plan.value
    if (!current || busy.value) return
    const owner = generation
    busy.value = true; error.value = ''
    try {
      const updated = await next(current)
      if (owner !== generation) return
      if (direct && commitsDirectly(updated.baseline)) { finish(await commitPlan(updated.id, updated.hash, none())); return }
      plan.value = updated
    } catch (e) { if (owner === generation) error.value = String(e) }
    finally { if (owner === generation) busy.value = false }
  }
  const merge = (target: number) => replan(p => App.ReplanAdmissionGroupChoice(p.id, p.hash, 'merge', target))
  const join = (target: number) => replan(p => App.ReplanAdmissionGroupChoice(p.id, p.hash, 'join', target))
  const separate = () => replan(p => App.ReplanAdmissionGroupChoice(p.id, p.hash, 'separate', 0))
  /** 「使用发行 A」：新计划不再需要确认时直接提交。 */
  const useProvider = (releaseId: number) => replan(p => App.UseSongProvider(p.id, p.hash, releaseId), true)
  async function refreshCoverage() {
    const current = plan.value
    if (!current || busy.value || fetching.value) return
    const owner = generation
    busy.value = true; fetching.value = true; coverageError.value = ''
    try { const updated = await App.RefreshAdmissionCoverage(current.id, current.hash); if (owner === generation) plan.value = updated }
    catch (e) { if (owner === generation) coverageError.value = String(e) }
    finally { if (owner === generation) { busy.value = false; fetching.value = false } }
  }
  async function confirm(word = '') {
    const current = plan.value
    if (!current || busy.value || current.baseline.conflict || error.value) return
    if (current.baseline.needsTypedConfirm && !typedWordMatches(word)) return
    busy.value = true
    try {
      const approval = current.baseline.needsTypedConfirm
        ? await App.ConfirmTypedAdmission(current.id, current.hash, word)
        : current.baseline.requiresConfirmation
          ? await App.ConfirmReleaseAdditions(current.id, current.hash)
          : none()
      finish(await commitPlan(current.id, current.hash, approval))
    } catch (e) { error.value = String(e); busy.value = false }
  }
  function cancel() { if (!busy.value) finish(null) }
  return { plan, busy, error, fetching, coverageError, run, refresh, merge, join, separate, useProvider, refreshCoverage, confirm, cancel }
})
