import type { ViewKind } from '@/stores/library'

export type FilterCategory = 'playlistIds' | 'tagIds' | 'syncedPlaylistIds'
export const categoryLabels: Record<FilterCategory, string> = {
  playlistIds: '歌单', tagIds: '标签', syncedPlaylistIds: '同步歌单',
}
export const supportsLibraryScope = (view: ViewKind) =>
  ['artists', 'artist', 'albums', 'album', 'recent', 'syncedPlaylist'].includes(view)
export const supportsSyncedCategory = (view: ViewKind) =>
  ['artists', 'artist', 'albums', 'album', 'recent'].includes(view)
export const defaultLibraryOnly = (view: ViewKind) => view !== 'syncedPlaylist'

/** null 是不限制；[] 是没有匹配。点击全选中的一项取消它，其余仍选中。 */
export function toggleCategory(current: number[] | null | undefined, id: number, options: number[]): number[] | null {
  const next = new Set(current ?? options)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  return options.length > 0 && options.every(value => next.has(value)) ? null : [...next]
}
