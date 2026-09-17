import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import { trackColumns, useTrackColumnsStore } from '../../src/stores/trackColumns'

let storage: Map<string, string>
beforeEach(() => {
  storage = new Map()
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => storage.set(key, value),
  })
  setActivePinia(createPinia())
})
afterEach(() => vi.unstubAllGlobals())

describe('track column preferences', () => {
  it.each(['broken', '[]', '["status"]', '["position"]', '{}'])('invalid preferences %s restore existing columns', saved => {
    storage.set('sonexa.trackColumns', saved)
    expect(useTrackColumnsStore().visible).toEqual(trackColumns.map(column => column.key))
  })
  it('filters unknown keys and duplicates; last column cannot be hidden', async () => {
    storage.set('sonexa.trackColumns', '["title","status","position","title"]')
    const columns = useTrackColumnsStore()
    expect(columns.visible).toEqual(['title'])
    columns.toggle('title')
    expect(columns.visible).toEqual(['title'])
    columns.toggle('cover')
    await nextTick()
    expect(JSON.parse(storage.get('sonexa.trackColumns')!)).toEqual(['title', 'cover'])
    expect(columns.minWidth).toBe(198)
  })
  it('unavailable storage does not prevent toggles', async () => {
    vi.stubGlobal('localStorage', {
      getItem: () => { throw new Error('blocked') },
      setItem: () => { throw new Error('blocked') },
    })
    const columns = useTrackColumnsStore()
    columns.toggle('cover')
    await nextTick()
    expect(columns.has('cover')).toBe(false)
  })
})
