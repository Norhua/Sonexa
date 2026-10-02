import { valueOf, type EditValue } from './useEditDraft'

export const editorLabels: Record<string, string> = {
  'track.title': '曲名', 'track.credit': '歌曲署名', 'track.year': '年份',
  'track.discNumber': '碟号', 'track.trackNumber': '音轨号', 'track.genre': '风格',
  'track.lyrics': '歌词', 'track.roles.vocal': '演唱者', 'track.roles.composer': '作曲',
  'track.roles.lyricist': '作词', 'release.title': '专辑名', 'release.credit': '专辑艺术家',
  'release.primaryType': '类型', 'release.secondaryTypes': '其他类型',
  'release.discTotal': '碟数', 'release.media': '每碟曲数',
}

// 只列有展示意义的事实，身份、缓存、文件引用和清单交给各自的交互处理。
export const editorFactLabels: Record<string, string> = {
  'track.durationMS': '时长', 'track.identifiers': '歌曲行业标识',
  'track.trackNumberText': '上游曲序', 'track.roles.arranger': '编曲',
  'track.roles.producer': '制作', 'track.roles.mixer': '混音',
  'track.roles.conductor': '指挥', 'track.roles.instrument': '器乐演奏',
  'track.roles.performer': '演出', 'release.country': '发行国家',
  'release.date': '发行日期', 'release.status': '发行状态',
  'release.labels': '厂牌与目录号', 'release.identifiers': '发行行业标识',
  'release.annotation': '发行注释', 'release.releaseTrackTotal': '发行总曲数',
  'group.firstReleaseDate': '首次发行日期',
}

export function displayEditorValue(value: unknown): string {
  if (value === null || value === undefined) return '未知'
  if (Array.isArray(value)) return value.map(displayEditorValue).join('、') || '空列表'
  if (typeof value === 'object') {
    const item = value as Record<string, any>
    if (item.state) return item.state === 'value' ? displayEditorValue(item.value) : item.state === 'empty' ? '已清空' : '未知'
    if (item.kind) return `${item.kind}：${displayEditorValue(item.value)}`
    if (item.artist) return `${item.artist.name}${item.attributes?.length ? `（${item.attributes.join('、')}）` : ''}`
    if (item.number) return `第 ${item.number} 碟 · ${displayEditorValue(item.trackTotal)} 首${item.format?.state === 'value' ? ` · ${item.format.value}` : ''}`
    if (item.catalogNumber) return [item.name, item.catalogNumber].filter(Boolean).join(' · ')
    return item.text ?? item.name ?? '已提供'
  }
  return String(value)
}

export function editorFieldText(field?: EditValue, key?: string): string {
  if (field?.state === 'empty') return '已清空'
  const value = valueOf(field)
  if (key === 'track.durationMS' && typeof value === 'number') {
    const seconds = Math.floor(value / 1000)
    return `${Math.floor(seconds / 60)}:${String(seconds % 60).padStart(2, '0')}`
  }
  if (key === 'release.primaryType' && typeof value === 'string') {
    return ({ album: '专辑', single: '单曲', ep: 'EP', broadcast: '广播', other: '其他' } as Record<string, string>)[value.toLowerCase()] ?? value
  }
  return displayEditorValue(value)
}

export function sameEditorValue(a?: EditValue, b?: EditValue): boolean {
  return a?.state === b?.state && JSON.stringify(valueOf(a)) === JSON.stringify(valueOf(b))
}
