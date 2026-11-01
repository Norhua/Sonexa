import { ref } from 'vue'
import { App, type Models } from '@/api'

type Scope = Models.store.ProblemScope
type Lease = Models.domain.ProblemLease

/** Lease ownership is separate from queue snapshots. Late acquisitions are
 * released, and renewals never read queue data or change approval revisions. */
export function useProblemLease() {
  const scope = ref<Scope | null>(null)
  const pending = ref(false)
  const error = ref('')
  let lease: Lease | null = null
  let generation = 0
  let timer: ReturnType<typeof setTimeout> | null = null
  let opening: Promise<void> | null = null

  async function release(value: Lease) {
    try { await App.ReleaseReviewProblemLease(value) }
    catch { /* Closed/expired scopes are already unusable; server TTL is a backstop. */ }
  }
  function stopTimer() { if (timer) clearTimeout(timer); timer = null }
  function schedule(owner: number) {
    stopTimer()
    timer = setTimeout(async () => {
      const current = lease
      if (!current || owner !== generation) return
      try {
        const next = await App.RenewReviewProblemLease(current)
        if (owner !== generation) return
        lease = next
        schedule(owner)
      } catch (e) {
        if (owner !== generation) return
        scope.value = null; lease = null; error.value = String(e)
        void release(current)
      }
    }, 30_000)
  }
  function close() {
    generation++; stopTimer()
    const previous = lease
    lease = null; scope.value = null; pending.value = false; opening = null
    return previous ? release(previous) : Promise.resolve()
  }
  function open(next: Scope): Promise<void> {
    const released = close()
    const owner = generation
    pending.value = true; error.value = ''
    opening = (async () => {
      try {
        await released
        if (owner !== generation) return
        const interaction = await App.AcquireReviewProblemLease(next)
        if (owner !== generation) { await release(interaction.lease); return }
        lease = interaction.lease; scope.value = interaction.scope
        schedule(owner)
      } catch (e) { if (owner === generation) error.value = String(e) }
      finally { if (owner === generation) pending.value = false }
    })()
    return opening
  }
  function update(next: Scope) {
    if (!lease || next.problemId !== lease.problemId || (next.leaseToken && next.leaseToken !== lease.token)) return false
    scope.value = { ...next, leaseToken: lease.token } as Scope
    return true
  }
  return { scope, pending, error, open, close, update }
}
