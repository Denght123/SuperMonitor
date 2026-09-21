import { afterEach, describe, expect, it, vi } from 'vitest'

import { api } from './client'

describe('api.deleteAccount', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('sends an encoded DELETE request and accepts a 204 response', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.deleteAccount('codex/account 1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts/codex%2Faccount%201', expect.objectContaining({ method: 'DELETE' }))
  })
})

describe('administrator authentication API', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('exchanges the password through same-origin credentials without persisting it', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ authenticated: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.login('a-strong-administrator-password')).resolves.toEqual({ authenticated: true })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/auth/session', expect.objectContaining({
      method: 'POST',
      credentials: 'same-origin',
      body: JSON.stringify({ password: 'a-strong-administrator-password', remember: true }),
    }))
  })
})

describe('notification API', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('creates a Feishu channel without changing the submitted secret', async () => {
    const channel = { id: 'channel-1', kind: 'feishu', name: '开发组', target: '飞书自定义机器人 · Webhook 已加密', enabled: true, createdAt: '2026-09-19T00:00:00Z', updatedAt: '2026-09-19T00:00:00Z' }
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(channel), { status: 201, headers: { 'Content-Type': 'application/json' } }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.createNotificationChannel({ kind: 'feishu', name: '开发组', webhookUrl: 'https://open.feishu.cn/open-apis/bot/v2/hook/secret' })).resolves.toEqual(channel)
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/notifications/channels', expect.objectContaining({
      method: 'POST',
      body: JSON.stringify({ kind: 'feishu', name: '开发组', webhookUrl: 'https://open.feishu.cn/open-apis/bot/v2/hook/secret' }),
    }))
  })

  it('accepts a 204 response when deleting a notification channel', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(null, { status: 204 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(api.deleteNotificationChannel('channel/1')).resolves.toBeUndefined()
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/notifications/channels/channel%2F1', expect.objectContaining({ method: 'DELETE' }))
  })
})

describe('Codex usage import API', () => {
  afterEach(() => vi.restoreAllMocks())

  it('submits only extracted aggregate fields', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ importedSources: 1, unchangedSources: 0, importedEntries: 1, importedTokens: 1200 }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    await api.importCodexUsage('codex/account', [{
      sourceId: 'session-sol', contentHash: 'hash-v1',
      entries: [{ date: '2026-09-21', model: 'gpt-5.6-sol', counters: { inputTokens: 700, outputTokens: 200, cacheTokens: 300, requests: 1 } }],
    }])
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/accounts/codex%2Faccount/usage/codex-import', expect.objectContaining({
      method: 'POST',
      body: expect.not.stringContaining('prompt'),
    }))
  })
})
