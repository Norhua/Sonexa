import { computed, ref, watch } from 'vue'
import { defineStore } from 'pinia'

/** 只控制既有列的可见性；不影响排序、选择、导出字段或歌单顺序。 */
export const trackColumns = [
  { key: 'trackNumber', label: '音轨号', width: 72 },
  { key: 'cover', label: '封面', width: 48 },
  { key: 'title', label: '标题', width: 150 },
  { key: 'artist', label: '艺术家', width: 150 },
  { key: 'album', label: '专辑', width: 150 },
  { key: 'duration', label: '时长', width: 76 },
  { key: 'year', label: '年份', width: 76 },
  { key: 'medium', label: '载体', width: 80 },
  { key: 'country', label: '国家', width: 64 },
  { key: 'addedAt', label: '加入时间', width: 176 },
] as const
export type TrackColumnKey = typeof trackColumns[number]['key']
export type TrackColumnContext = 'library' | 'album'
const storageKey = 'sonexa.trackColumns'
const defaults = (context: TrackColumnContext) => trackColumns
  .filter(column => column.key !== 'addedAt' && (context === 'album' || column.key !== 'trackNumber'))
  .map(column => column.key)

function load(context: TrackColumnContext): TrackColumnKey[] {
  try {
    const key = context === 'album' ? `${storageKey}.album` : storageKey
    const saved: unknown = JSON.parse(localStorage.getItem(key) ?? 'null')
    if (Array.isArray(saved)) {
      const keys = trackColumns.map(column => column.key).filter(key => saved.includes(key))
      if (keys.length) return keys
    }
  } catch { /* 损坏或不可用的本地存储不阻止浏览。 */ }
  return defaults(context)
}

export const useTrackColumnsStore = defineStore('trackColumns', () => {
  const visible = ref(load('library'))
  const albumVisible = ref(load('album'))
  const visibleFor = (context: TrackColumnContext = 'library') => context === 'album' ? albumVisible.value : visible.value
  const widthFor = (context: TrackColumnContext = 'library') => trackColumns
    .filter(column => visibleFor(context).includes(column.key)).reduce((width, column) => width + column.width, 0)
  const minWidth = computed(() => widthFor())
  const has = (key: TrackColumnKey, context: TrackColumnContext = 'library') => visibleFor(context).includes(key)
  function toggle(key: TrackColumnKey, context: TrackColumnContext = 'library') {
    const target = context === 'album' ? albumVisible : visible
    if (has(key, context)) {
      if (target.value.length > 1) target.value = target.value.filter(value => value !== key)
    } else target.value = [...target.value, key]
  }
  for (const [key, target] of [[storageKey, visible], [`${storageKey}.album`, albumVisible]] as const) {
    watch(target, value => {
      try { localStorage.setItem(key, JSON.stringify(value)) } catch { /* 当前会话仍生效。 */ }
    })
  }
  return { visible, albumVisible, visibleFor, widthFor, minWidth, has, toggle }
})
