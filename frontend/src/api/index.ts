// 前端与 Go 侧的唯一接口层。
//
// 前端不自行发出任何出网请求——全部出网经由这些绑定方法，
// 以便统一管控 User-Agent 与限流。封面图片是唯一特例：
// 仍由 Go 取得，但以本地地址供 <img> 加载（见 coverUrl）。
export * as App from '../../wailsjs/go/app/App'
export { EventsOn, EventsOff } from '../../wailsjs/runtime/runtime'
export type * as Models from '../../wailsjs/go/models'

/** 后端推送的事件名。 */
export const Events = {
  ImportProgress: 'import:progress',
  EnrichProgress: 'enrich:progress',
  SettingsSaved: 'settings:saved',
  LibraryChanged: 'library:changed',
  CoverChanged: 'cover:changed',
  CoverResults: 'cover:results',
  ProviderResults: 'upstream:providerResults',
} as const

/** 封面解析用途与 CSS 展示尺寸分离。 */
export type CoverUsage = 'thumbnail' | 'detail'

/** 库内歌集或稳定的上游发行身份；不接受远程图片 URL。 */
export interface CoverSource {
  releaseId?: number | null
  revision?: number
  provider?: string | null
  /** MusicBrainz release MBID 或带地区的 iTunes 专辑身份。 */
  ref?: string | null
  /** release group MBID，降级链的最后一档要用。 */
  rg?: string | null
}

/**
 * 构造封面的本地地址；来源不全时返回空串。
 *
 * 字节由 Go 取得并写入磁盘缓存，这里只给出本地 URL——
 * 不经 IPC 桥回传字节，因此 <img> 的懒加载与解码不占主线程都保留。
 *
 * 地址里带的是**来源**而不是缓存键。键是来源的哈希、不可逆，
 * 后端拿着键构造不出降级链，只能依赖前端先调预取把链登记进来——
 * 而资料库表格、详情抽屉、复核队列都不调预取，搜索页的预取又比
 * <img> 的请求晚到。那正是此前所有位置的封面都加载不出来的成因。
 */
export function coverUrl(
  source: CoverSource | null | undefined,
  usage: CoverUsage = 'thumbnail',
): string {
  if (!source?.releaseId && (!source?.provider || !source.ref)) return ''
  const q = new URLSearchParams({ usage })
  if (source.releaseId) q.set('releaseId', String(source.releaseId))
  else {
    q.set('provider', source.provider ?? '')
    q.set('ref', source.ref ?? '')
    if (source.rg) q.set('rg', source.rg)
  }
  q.set('revision', String(source.revision ?? 0))
  return `/cover?${q.toString()}`
}

/** 仅供已登记历史文件的迁移兼容入口。 */
export function adoptedCoverUrl(path: string): string {
  if (!path) return ''
  return `/cover?adopted=${encodeURIComponent(path)}`
}

/** 缺失值的占位呈现。缺失与空值必须可区分，不用 0 或空白伪装。 */
export const DASH = '—'

/** 把可空值渲染为显示文本，缺失时为占位短横线。 */
export function display(v: string | number | null | undefined): string {
  if (v === null || v === undefined || v === '') return DASH
  return String(v)
}

/** 加入时间按本地时区显示 yyyy-mm-dd；缺失或非法值显示短横线。 */
export function addedTime(value: string | null | undefined): string {
  if (!value) return DASH
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return DASH
  return [
    String(date.getFullYear()).padStart(4, '0'),
    String(date.getMonth() + 1).padStart(2, '0'),
    String(date.getDate()).padStart(2, '0'),
  ].join('-')
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
  const part = (value: number | null | undefined) => value != null && value > 0 ? String(value) : DASH
  if ((n == null || n <= 0) && (total == null || total <= 0)) return DASH
  return `${part(n)}/${part(total)}`
}
