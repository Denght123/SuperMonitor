import type { components } from './schema'

export type Overview = components['schemas']['Overview']
export type AccountSummary = components['schemas']['AccountSummary']
export type Alert = components['schemas']['Alert']
export type QuotaSignal = components['schemas']['QuotaSignal']
export type Provider = components['schemas']['Provider']
export type DeviceLoginSession = components['schemas']['DeviceLoginSession']
export type ActivityItem = components['schemas']['Activity']

type APIErrorShape = {
  error?: {
    code?: string
    message?: string
  }
}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
  ) {
    super(message)
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  })

  if (!response.ok) {
    const payload = (await response.json().catch(() => ({}))) as APIErrorShape
    throw new APIError(
      payload.error?.message ?? `Request failed with status ${response.status}`,
      response.status,
      payload.error?.code ?? 'request_failed',
    )
  }

  return (await response.json()) as T
}

export const api = {
  overview: () => request<Overview>('/overview'),
  providers: () => request<{ items: Provider[] }>('/providers'),
  refresh: () => request<{ status: string; message: string }>('/refresh', { method: 'POST' }),
  importCredential: (providerId: string, file: File, alias: string) => {
    const body = new FormData()
    body.append('file', file)
    if (alias.trim()) body.append('alias', alias.trim())
    return request<AccountSummary>(`/providers/${encodeURIComponent(providerId)}/accounts/import`, { method: 'POST', body })
  },
  connectSecret: (providerId: string, alias: string, secret: string, secret2 = '') => request<AccountSummary>(`/providers/${encodeURIComponent(providerId)}/accounts/secret`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ alias: alias.trim(), secret, secret2 }),
  }),
  startCodexDeviceLogin: (alias: string) => request<DeviceLoginSession>('/providers/codex/device-login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ alias: alias.trim() }),
  }),
  codexDeviceLoginStatus: (id: string) => request<DeviceLoginSession>(`/providers/codex/device-login/${encodeURIComponent(id)}`),
  startProviderOAuth: (providerId: string, alias: string) => request<DeviceLoginSession>(`/providers/${encodeURIComponent(providerId)}/oauth`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ alias: alias.trim() }),
  }),
  providerOAuthStatus: (providerId: string, id: string) => request<DeviceLoginSession>(`/providers/${encodeURIComponent(providerId)}/oauth/${encodeURIComponent(id)}`),
  refreshAccount: (id: string) => request<AccountSummary>(`/accounts/${encodeURIComponent(id)}/refresh`, { method: 'POST' }),
  activities: () => request<{ items: ActivityItem[] }>('/activities'),
  runActivity: (accountId: string, activityId: string) => request<ActivityItem>(`/activities/${encodeURIComponent(accountId)}/${encodeURIComponent(activityId)}`, { method: 'POST' }),
}
