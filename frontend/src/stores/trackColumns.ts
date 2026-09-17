import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

/** 只控制既有列的可见性；不影响排序、选择、导出字段或歌单顺序。 */
export const trackColumns = [
  { key: 'position', label: '曲序', width: 44 },
  { key: 'cover', label: '封面', width: 48 },
  { key: 'title', label: '标题', width: 150 },
  { key: 'artist', label: '艺术家', width: 150 },
  { key: 'album', label: '专辑', width: 150 },
  { key: 'duration', label: '时长', width: 76 },
  { key: 'year', label: '年份', width: 76 },
  { key: 'medium', label: '载体', width: 80 },
  { key: 'country', label: '国家', width: 64 },
] as const
export type TrackColumnKey = typeof trackColumns[number]['key']
const storageKey = 'sonexa.trackColumns'
const defaults = () => trackColumns.map(column => column.key)

function load(): TrackColumnKey[] {
  try {
    const saved: unknown = JSON.parse(localStorage.getItem(storageKey) ?? 'null')
    if (Array.isArray(saved)) {
      const keys = defaults().filter(key => saved.includes(key))
      if (keys.length) return keys
    }
  } catch { /* 损坏或不可用的本地存储不阻止浏览。 */ }
  return defaults()
}

export const useTrackColumnsStore = defineStore('trackColumns', () => {
  const visible = ref(load())
  const minWidth = computed(() => trackColumns.filter(column => visible.value.includes(column.key))
    .reduce((width, column) => width + column.width, 0))
  const has = (key: TrackColumnKey) => visible.value.includes(key)
  function toggle(key: TrackColumnKey) {
    if (has(key)) {
      if (visible.value.length > 1) visible.value = visible.value.filter(value => value !== key)
    } else visible.value = [...visible.value, key]
  }
  watch(visible, value => {
    try { localStorage.setItem(storageKey, JSON.stringify(value)) } catch { /* 当前会话仍生效。 */ }
  })
  return { visible, minWidth, has, toggle }
})
