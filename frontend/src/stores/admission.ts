import { defineStore } from 'pinia'
import { ref } from 'vue'
import { App, type Models } from '@/api'

type Plan = Models.app.AdmissionPlanView
type Result = Models.domain.AdmissionResult
/** 所有交互入库共用同一计划确认；取消返回 null，来源页面保持原选择。 */
export const useAdmissionStore = defineStore('admission', () => {
  const plan = ref<Plan | null>(null)
  const busy = ref(false)
  const error = ref('')
  let prepare: (() => Promise<Plan>) | null = null
  let settle: ((result: Result | null) => void) | null = null
  let generation = 0
  let commitPlan = App.CommitAdmission
  function finish(result: Result | null) {
    generation++
    plan.value = null
    busy.value = false
    prepare = null
    const done = settle; settle = null; done?.(result)
  }
  async function run(next: () => Promise<Plan>, commit = App.CommitAdmission): Promise<Result | null> {
    if (busy.value || plan.value) throw new Error('请先完成当前入库确认')
    commitPlan = commit
    busy.value = true; error.value = ''
    try {
      const first = await next()
      if (!first.baseline.conflict && !first.baseline.requiresConfirmation) {
        try { return await commitPlan(first.id, first.hash, {} as Models.domain.AdmissionApproval) }
        catch (e) { error.value = String(e) }
      }
      prepare = next
      plan.value = first
      return await new Promise<Result | null>(resolve => { settle = resolve; busy.value = false })
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
  async function merge(target: number) {
    if (!plan.value || busy.value) return
    busy.value = true; error.value = ''
    try { plan.value = await App.ReplanAdmissionGroup(plan.value.id, plan.value.hash, target) }
    catch (e) { error.value = String(e) }
    finally { busy.value = false }
  }
  async function confirm() {
    const current = plan.value
    if (!current || busy.value || current.baseline.conflict || error.value) return
    busy.value = true
    try {
      const approval = current.baseline.requiresConfirmation
        ? await App.ConfirmReleaseAdditions(current.id, current.hash)
        : {} as Models.domain.AdmissionApproval
      finish(await commitPlan(current.id, current.hash, approval))
    } catch (e) { error.value = String(e); busy.value = false }
  }
  function cancel() { if (!busy.value) finish(null) }
  return { plan, busy, error, run, refresh, merge, confirm, cancel }
})
