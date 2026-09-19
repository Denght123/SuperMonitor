import { describe, expect, it } from 'vitest'
import { filterUsageByRange, usageRangeDescription } from './usageRange'

const usage = Array.from({ length: 400 }, (_, index) => {
  const date = new Date(Date.UTC(2025, 8, 19 + index))
  return { date: date.toISOString().slice(0, 10), requests: index }
})

describe('usage range helpers', () => {
  const now = new Date('2026-09-19T15:30:00+08:00')

  it('keeps the selected inclusive calendar window', () => {
    const result = filterUsageByRange(usage, '7d', now)
    expect(result).toHaveLength(7)
    expect(result[0].date).toBe('2026-09-13')
    expect(result.at(-1)?.date).toBe('2026-09-19')
  })

  it('supports 30-day and one-year windows without truncating all history', () => {
    expect(filterUsageByRange(usage, '30d', now)).toHaveLength(30)
    expect(filterUsageByRange(usage, '1y', now)).toHaveLength(365)
    expect(filterUsageByRange(usage, 'all', now)).toHaveLength(400)
  })

  it('drops invalid and future dates from bounded ranges', () => {
    const mixed = [{ date: 'invalid' }, { date: '2026-09-19' }, { date: '2026-09-20' }]
    expect(filterUsageByRange(mixed, '7d', now)).toEqual([{ date: '2026-09-19' }])
  })

  it('describes the selected range and actual data coverage', () => {
    expect(usageRangeDescription('30d', 12)).toBe('30 天 · 12 个数据日')
    expect(usageRangeDescription('all', 400)).toBe('全部历史 · 400 个数据日')
  })
})
