import { App, type CoverSource } from '@/api'
export type CoverOutcome = 'empty' | 'error' | 'disabled'
interface Pending {
  source: CoverSource
  usage: 'thumbnail' | 'detail'
  revision: number
  resolve: (outcome: CoverOutcome) => void
}
let pending: Pending[] = []
let timer: ReturnType<typeof setTimeout> | null = null
async function flush() {
  timer = null
  const batch = pending; pending = []
  try {
    const got = await App.CoverTargetOutcomes(batch.map(p => ({
      releaseId: p.source.releaseId ?? 0, provider: p.source.provider ?? '', ref: p.source.ref ?? '',
      groupRef: p.source.rg ?? '', usage: p.usage, revision: p.revision,
    })))
    batch.forEach((p, i) => p.resolve(got[i] === 'empty' ? 'empty' : got[i] === 'disabled' ? 'disabled' : 'error'))
  } catch { batch.forEach(p => p.resolve('error')) }
}
export function coverOutcome(source: CoverSource, usage: 'thumbnail' | 'detail', revision: number): Promise<CoverOutcome> {
  return new Promise(resolve => {
    pending.push({ source: { ...source }, usage, revision, resolve })
    if (timer === null) timer = setTimeout(flush, 30)
  })
}
