export type ProviderOrderDirection = 'up' | 'down'

export const providerOrderStorageKey = 'supermonitor-provider-order'

export function normalizeProviderOrder(order: readonly string[], providerIds: readonly string[]) {
  const result: string[] = []
  const seen = new Set<string>()

  for (const id of [...order, ...providerIds]) {
    if (!id || seen.has(id)) continue
    seen.add(id)
    result.push(id)
  }
  return result
}

export function sortProviderEntries<T>(entries: readonly (readonly [string, T])[], order: readonly string[]) {
  const positions = new Map(order.map((id, index) => [id, index]))
  return entries
    .map((entry, index) => ({ entry, index }))
    .sort((left, right) => {
      const leftPosition = positions.get(left.entry[0]) ?? Number.MAX_SAFE_INTEGER
      const rightPosition = positions.get(right.entry[0]) ?? Number.MAX_SAFE_INTEGER
      return leftPosition - rightPosition || left.index - right.index
    })
    .map(({ entry }) => entry)
}

export function moveProviderInOrder(
  order: readonly string[],
  visibleProviderIds: readonly string[],
  providerId: string,
  direction: ProviderOrderDirection,
) {
  const normalized = normalizeProviderOrder(order, visibleProviderIds)
  const visibleSet = new Set(visibleProviderIds)
  const visible = normalized.filter((id) => visibleSet.has(id))
  const currentIndex = visible.indexOf(providerId)
  const nextIndex = direction === 'up' ? currentIndex - 1 : currentIndex + 1

  if (currentIndex < 0 || nextIndex < 0 || nextIndex >= visible.length) return normalized
  ;[visible[currentIndex], visible[nextIndex]] = [visible[nextIndex], visible[currentIndex]]
  return [...visible, ...normalized.filter((id) => !visibleSet.has(id))]
}

export function readProviderOrder(storage: Pick<Storage, 'getItem'> = localStorage) {
  try {
    const value = JSON.parse(storage.getItem(providerOrderStorageKey) ?? '[]') as unknown
    return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string' && item.length > 0) : []
  } catch {
    return []
  }
}

export function writeProviderOrder(order: readonly string[], storage: Pick<Storage, 'setItem'> = localStorage) {
  try {
    storage.setItem(providerOrderStorageKey, JSON.stringify(order))
  } catch {
    // A disabled or full localStorage must not block the monitoring interface.
  }
}
