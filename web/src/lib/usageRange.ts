export type UsageRange = '7d' | '30d' | '1y' | 'all'

export const usageRangeOptions: ReadonlyArray<{ value: UsageRange; label: string }> = [
  { value: '7d', label: '7 天' },
  { value: '30d', label: '30 天' },
  { value: '1y', label: '一年' },
  { value: 'all', label: '全部' },
]

type DatedUsage = { date: string }

function utcDay(value: Date) {
  return Date.UTC(value.getUTCFullYear(), value.getUTCMonth(), value.getUTCDate())
}

function usageDay(value: string) {
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(value)
  if (!match) return Number.NaN
  return Date.UTC(Number(match[1]), Number(match[2]) - 1, Number(match[3]))
}

export function filterUsageByRange<T extends DatedUsage>(items: readonly T[], range: UsageRange, now = new Date()): T[] {
  if (range === 'all') return [...items]

  const days = range === '7d' ? 7 : range === '30d' ? 30 : 365
  const end = utcDay(now)
  const start = end - (days - 1) * 24 * 60 * 60 * 1000

  return items.filter((item) => {
    const day = usageDay(item.date)
    return Number.isFinite(day) && day >= start && day <= end
  })
}

export function usageRangeDescription(range: UsageRange, visibleDays: number) {
  if (range === 'all') return visibleDays ? `全部历史 · ${visibleDays} 个数据日` : '全部历史'
  const label = usageRangeOptions.find((option) => option.value === range)?.label ?? range
  return `${label} · ${visibleDays} 个数据日`
}
