import { describe, expect, it } from 'vitest'
import { compactNumber, formatMetric, quotaValue } from './format'

describe('format helpers', () => {
  it('formats large token counts compactly', () => {
    expect(compactNumber(12_400_000)).toMatch(/1240万|12\.4M/i)
  })

  it('keeps account counts exact', () => {
    expect(formatMetric(5, 'accounts')).toBe('5')
  })

  it('formats currency balances', () => {
    expect(quotaValue(128.42, 'CNY')).toBe('¥128.42')
  })
})
