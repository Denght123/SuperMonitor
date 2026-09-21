export type CodexUsageCounters = {
  inputTokens: number
  outputTokens: number
  cacheTokens: number
  requests: number
}

export type CodexUsageImportEntry = {
  date: string
  model: string
  counters: CodexUsageCounters
}

export type CodexUsageImportSource = {
  sourceId: string
  contentHash: string
  entries: CodexUsageImportEntry[]
}

export type CodexUsageParseResult = {
  sources: CodexUsageImportSource[]
  acceptedFiles: number
  skippedFiles: number
  skippedEvents: number
  tokens: number
}

type JSONObject = Record<string, unknown>

const emptyCounters = (): CodexUsageCounters => ({ inputTokens: 0, outputTokens: 0, cacheTokens: 0, requests: 0 })

export async function parseCodexUsageFiles(files: File[]): Promise<CodexUsageParseResult> {
  const result: CodexUsageParseResult = { sources: [], acceptedFiles: 0, skippedFiles: 0, skippedEvents: 0, tokens: 0 }
  for (const file of files) {
    if (!file.name.toLowerCase().endsWith('.jsonl')) {
      result.skippedFiles++
      continue
    }
    const parsed = await parseCodexUsageText(await file.text(), file.webkitRelativePath || file.name)
    result.skippedEvents += parsed.skippedEvents
    if (!parsed.source) {
      result.skippedFiles++
      continue
    }
    result.sources.push(parsed.source)
    result.acceptedFiles++
    result.tokens += parsed.source.entries.reduce((sum, entry) => sum + entry.counters.inputTokens + entry.counters.outputTokens + entry.counters.cacheTokens, 0)
  }
  return result
}

export async function parseCodexUsageText(text: string, fallbackName = 'rollout.jsonl'): Promise<{ source: CodexUsageImportSource | null; skippedEvents: number }> {
  let sourceID = ''
  let currentModel = ''
  let skippedEvents = 0
  let previousTotal: CodexUsageCounters | null = null
  const aggregates = new Map<string, CodexUsageImportEntry>()

  for (const line of text.split(/\r?\n/)) {
    if (!line.includes('{')) continue
    let root: JSONObject
    try {
      const value: unknown = JSON.parse(line)
      if (!isObject(value)) continue
      root = value
    } catch {
      continue
    }
    const payload = objectValue(root.payload)
    if (stringValue(root.type) === 'session_meta') sourceID = stringValue(payload?.id)
    const nextModel = stringValue(payload?.model) || stringValue(root.model)
    if (nextModel) currentModel = nextModel

    const isTokenEvent = stringValue(root.type) === 'token_count' || stringValue(payload?.type) === 'token_count'
    if (!isTokenEvent) continue
    const info = objectValue(payload?.info)
    const lastUsage = objectValue(info?.last_token_usage) ?? objectValue(info?.lastTokenUsage) ?? objectValue(root.usage)
    const totalUsage = objectValue(info?.total_token_usage) ?? objectValue(info?.totalTokenUsage)
    let counters = lastUsage ? readCounters(lastUsage) : null
    if (!counters && totalUsage) {
      const total = readCounters(totalUsage)
      if (total) counters = previousTotal ? subtractCounters(total, previousTotal) : total
    }
    if (totalUsage) previousTotal = readCounters(totalUsage)
    if (!counters || counterTotal(counters) <= 0) continue
    if (!currentModel) {
      skippedEvents++
      continue
    }
    const date = timestampDate(stringValue(root.timestamp) || stringValue(root.created_at) || stringValue(root.createdAt))
    if (!date) {
      skippedEvents++
      continue
    }
    const key = `${date}\u0000${currentModel}`
    const entry = aggregates.get(key) ?? { date, model: currentModel, counters: emptyCounters() }
    entry.counters.inputTokens += counters.inputTokens
    entry.counters.outputTokens += counters.outputTokens
    entry.counters.cacheTokens += counters.cacheTokens
    entry.counters.requests += 1
    aggregates.set(key, entry)
  }

  const entries = [...aggregates.values()].sort((left, right) => left.date.localeCompare(right.date) || left.model.localeCompare(right.model))
  if (!entries.length) return { source: null, skippedEvents }
  const stableSourceID = sourceID || `file-${await sha256(fallbackName)}`
  return {
    source: {
      sourceId: stableSourceID,
      contentHash: await sha256(JSON.stringify(entries)),
      entries,
    },
    skippedEvents,
  }
}

function readCounters(value: JSONObject): CodexUsageCounters | null {
  const totalInput = nonNegativeNumber(value.input_tokens ?? value.inputTokens ?? value.input)
  const cached = nonNegativeNumber(value.cached_input_tokens ?? value.cachedInputTokens ?? value.cache_read_input_tokens ?? value.cacheReadInputTokens ?? value.cached)
  const output = nonNegativeNumber(value.output_tokens ?? value.outputTokens ?? value.output)
  if (totalInput === 0 && cached === 0 && output === 0) return null
  return { inputTokens: Math.max(0, totalInput - cached), outputTokens: output, cacheTokens: cached, requests: 0 }
}

function subtractCounters(current: CodexUsageCounters, previous: CodexUsageCounters): CodexUsageCounters {
  return {
    inputTokens: Math.max(0, current.inputTokens - previous.inputTokens),
    outputTokens: Math.max(0, current.outputTokens - previous.outputTokens),
    cacheTokens: Math.max(0, current.cacheTokens - previous.cacheTokens),
    requests: 0,
  }
}

function counterTotal(value: CodexUsageCounters) {
  return value.inputTokens + value.outputTokens + value.cacheTokens
}

function timestampDate(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.valueOf()) ? '' : date.toISOString().slice(0, 10)
}

function isObject(value: unknown): value is JSONObject {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function objectValue(value: unknown) {
  return isObject(value) ? value : undefined
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

function nonNegativeNumber(value: unknown) {
  const number = typeof value === 'number' ? value : typeof value === 'string' ? Number(value) : 0
  return Number.isFinite(number) && number > 0 ? Math.floor(number) : 0
}

async function sha256(value: string) {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(value))
  return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('')
}
