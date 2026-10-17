// Mirror the backend provider.QQRatePerMinute bounds for immediate form feedback.
// Defaults are read from App.DefaultSettings rather than replacing saved values.
export const QQ_RATE_MIN = 1
export const QQ_RATE_MAX = 60
export const QQ_RATE_ERROR = 'QQ 音乐合计速率必须是 1–60 请求/分钟内的有限数值'

export function validQQRate(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
    && value >= QQ_RATE_MIN && value <= QQ_RATE_MAX
}
