import { describe, expect, it } from 'vitest'
import { parseCodexUsageText } from './codexUsageImport'

describe('Codex local usage parser', () => {
  it('extracts the real turn model and non-overlapping token buckets', async () => {
    const text = [
      '{"type":"session_meta","payload":{"id":"session-sol"}}',
      '{"timestamp":"2026-09-21T01:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}',
      '{"timestamp":"2026-09-21T01:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":300,"output_tokens":200},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":300,"output_tokens":200}}}}',
    ].join('\n')
    const parsed = await parseCodexUsageText(text)
    expect(parsed.source?.sourceId).toBe('session-sol')
    expect(parsed.source?.entries).toEqual([{ date: '2026-09-21', model: 'gpt-5.6-sol', counters: { inputTokens: 700, outputTokens: 200, cacheTokens: 300, requests: 1 } }])
  })

  it('uses positive cumulative deltas when an older rollout has no last-token block', async () => {
    const text = [
      '{"type":"session_meta","payload":{"id":"session-delta"}}',
      '{"timestamp":"2026-09-21T01:00:00Z","type":"turn_context","payload":{"model":"gpt-5.6-sol"}}',
      '{"timestamp":"2026-09-21T01:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":10}}}}',
      '{"timestamp":"2026-09-21T01:02:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":30,"output_tokens":25}}}}',
    ].join('\n')
    const parsed = await parseCodexUsageText(text)
    expect(parsed.source?.entries[0].counters).toEqual({ inputTokens: 120, outputTokens: 25, cacheTokens: 30, requests: 2 })
  })

  it('does not invent a model for unattributed token events', async () => {
    const parsed = await parseCodexUsageText('{"timestamp":"2026-09-21T01:00:00Z","type":"token_count","usage":{"input_tokens":10,"output_tokens":2}}')
    expect(parsed.source).toBeNull()
    expect(parsed.skippedEvents).toBe(1)
  })
})
