import { describe, expect, it, vi } from 'vitest'

import { moveProviderInOrder, normalizeProviderOrder, providerOrderStorageKey, readProviderOrder, sortProviderEntries, writeProviderOrder } from './providerOrder'

describe('provider order', () => {
  it('deduplicates saved values and appends newly connected providers', () => {
    expect(normalizeProviderOrder(['codex', 'codex', 'zhipu'], ['zhipu', 'deepseek'])).toEqual(['codex', 'zhipu', 'deepseek'])
  })

  it('sorts groups by the saved order while keeping unknown groups stable', () => {
    const entries = [['deepseek', 1], ['codex', 2], ['zhipu', 3], ['new-provider', 4]] as const
    expect(sortProviderEntries(entries, ['zhipu', 'codex']).map(([id]) => id)).toEqual(['zhipu', 'codex', 'deepseek', 'new-provider'])
  })

  it('moves only visible groups and preserves hidden saved providers', () => {
    expect(moveProviderInOrder(['codex', 'hidden', 'zhipu', 'deepseek'], ['codex', 'zhipu', 'deepseek'], 'deepseek', 'up'))
      .toEqual(['codex', 'deepseek', 'zhipu', 'hidden'])
    expect(moveProviderInOrder(['codex', 'zhipu'], ['codex', 'zhipu'], 'codex', 'up')).toEqual(['codex', 'zhipu'])
  })

  it('reads and writes resiliently when storage data is malformed', () => {
    expect(readProviderOrder({ getItem: () => '{broken' })).toEqual([])
    expect(readProviderOrder({ getItem: () => JSON.stringify(['codex', 7, '', 'zhipu']) })).toEqual(['codex', 'zhipu'])

    const setItem = vi.fn()
    writeProviderOrder(['zhipu', 'codex'], { setItem })
    expect(setItem).toHaveBeenCalledWith(providerOrderStorageKey, JSON.stringify(['zhipu', 'codex']))
  })
})
