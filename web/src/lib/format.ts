export function compactNumber(value: number): string {
  return new Intl.NumberFormat('zh-CN', {
    notation: 'compact',
    maximumFractionDigits: 1,
  }).format(value)
}

export function formatMetric(value: number, unit: string): string {
  if (unit === 'tokens') return compactNumber(value)
  if (unit === 'requests') return compactNumber(value)
  if (unit === 'accounts' || unit === 'alerts') return value.toLocaleString('zh-CN')
  return `${compactNumber(value)} ${unit}`
}

export function relativeTime(value: string): string {
  const seconds = Math.round((new Date(value).getTime() - Date.now()) / 1000)
  const formatter = new Intl.RelativeTimeFormat('zh-CN', { numeric: 'auto' })
  const absolute = Math.abs(seconds)
  if (absolute < 60) return formatter.format(seconds, 'second')
  const minutes = Math.round(seconds / 60)
  if (Math.abs(minutes) < 60) return formatter.format(minutes, 'minute')
  const hours = Math.round(minutes / 60)
  if (Math.abs(hours) < 24) return formatter.format(hours, 'hour')
  return formatter.format(Math.round(hours / 24), 'day')
}

export function quotaValue(value: number, unit: string): string {
  if (unit === '%') return `${value.toFixed(0)}%`
  if (unit === 'CNY') return `¥ ${value.toFixed(2)}`
  if (unit === 'tokens') return compactNumber(value)
  if (unit === 'credits') return `${value.toFixed(1)}`
  return `${value}`
}
