// 前端与 Go 侧的唯一接口层。
//
// 前端不自行发出任何出网请求——全部出网经由这些绑定方法，
// 以便统一管控 User-Agent 与限流。封面图片是唯一特例：
// 仍由 Go 取得，但以本地地址供 <img> 加载（见 coverUrl）。
export * as App from '../../wailsjs/go/main/App'
export { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
export type * as Models from '../../wailsjs/go/models'

/** 后端推送的事件名。 */
export const Events = {
  ImportProgress: 'import:progress',
  SettingsSaved: 'settings:saved',
  LibraryChanged: 'library:changed',
} as const

/**
 * 构造封面的本地地址。
 *
 * 字节由 Go 取得并写入磁盘缓存，这里只给出本地 URL——
 * 不经 IPC 桥回传字节，因此 <img> 的懒加载、滚出视口自动取消、
 * 解码不占主线程等原生行为全部保留。
 */
export function coverUrl(key: string | undefined, size = 250): string {
  if (!key) return ''
  return `/cover?key=${encodeURIComponent(key)}&size=${size}`
}

/** 缺失值的占位呈现。缺失与空值必须可区分，不用 0 或空白伪装。 */
export const DASH = '—'

/** 把可空值渲染为显示文本，缺失时为占位短横线。 */
export function display(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return DASH
  return String(v)
}

/** 把毫秒渲染为 m:ss。 */
export function duration(ms: number | null | undefined): string {
  if (ms === null || ms === undefined || ms <= 0) return DASH
  const total = Math.round(ms / 1000)
  const m = Math.floor(total / 60)
  const s = total % 60
  return `${m}:${String(s).padStart(2, '0')}`
}

/** 把「第几曲/共几曲」渲染为 n/m。 */
export function trackPosition(
  n: number | null | undefined,
  total: number | null | undefined,
): string {
  if (n === null || n === undefined) return DASH
  if (total === null || total === undefined) return String(n)
  return `${n}/${total}`
}
