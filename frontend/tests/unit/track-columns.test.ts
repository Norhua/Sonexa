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
    expect(useTrackColumnsStore().visible).toEqual(trackColumns.filter(column => column.key !== 'trackNumber' && column.key !== 'addedAt').map(column => column.key))
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
  it('album defaults and preferences are independent, including last-column protection', async () => {
    const columns = useTrackColumnsStore()
    expect(columns.has('trackNumber')).toBe(false)
    expect(columns.has('trackNumber', 'album')).toBe(true)
    columns.toggle('trackNumber')
    columns.toggle('trackNumber', 'album')
    expect(columns.has('trackNumber')).toBe(true)
    expect(columns.has('trackNumber', 'album')).toBe(false)
    await nextTick()
    setActivePinia(createPinia())
    const reloaded = useTrackColumnsStore()
    expect(reloaded.has('trackNumber')).toBe(true)
    expect(reloaded.has('trackNumber', 'album')).toBe(false)
    for (const key of reloaded.albumVisible.filter(key => key !== 'title')) reloaded.toggle(key, 'album')
    reloaded.toggle('title', 'album')
    expect(reloaded.albumVisible).toEqual(['title'])
    expect(reloaded.widthFor('album')).toBe(150)
    expect(reloaded.visible).toContain('cover')
  })
  it('added time is opt-in in both contexts and track number precedes cover', async () => {
    const columns = useTrackColumnsStore()
    expect(trackColumns.slice(0, 2).map(column => column.key)).toEqual(['trackNumber', 'cover'])
    for (const context of ['library', 'album'] as const) {
      expect(columns.has('addedAt', context)).toBe(false)
      columns.toggle('addedAt', context)
    }
    await nextTick()
    setActivePinia(createPinia())
    const reloaded = useTrackColumnsStore()
    expect(reloaded.has('addedAt')).toBe(true)
    expect(reloaded.has('addedAt', 'album')).toBe(true)
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
