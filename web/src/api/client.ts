import type { components } from './schema'

export type Overview = components['schemas']['Overview']
export type AccountSummary = components['schemas']['AccountSummary']
export type Alert = components['schemas']['Alert']
export type QuotaSignal = components['schemas']['QuotaSignal']
export type Provider = components['schemas']['Provider']
export type DeviceLoginSession = components['schemas']['DeviceLoginSession']
export type ActivityItem = components['schemas']['Activity']

export type NotificationChannelKind = 'feishu' | 'qq_mail'

export type NotificationChannel = {
  id: string
  kind: NotificationChannelKind
  name: string
  target: string
  enabled: boolean
  createdAt: string
  updatedAt: string
}

export type NotificationPolicy = {
  lowQuotaPercent: number
  resetReminderDays: number[]
  schedulerMinutes: number
  autoActivities: boolean
}

export type NotificationEvaluation = {
  checkedAccounts: number
  triggeredAlerts: number
  deliveredMessages: number
  failedMessages: number
  availableActivities: number
  completedActivities: number
}

export type CodexUsageImportSource = {
  sourceId: string
  contentHash: string
  entries: Array<{
    date: string
    model: string
    counters: { inputTokens: number; outputTokens: number; cacheTokens: number; requests: number }
  }>
}

export type CodexUsageImportResult = {
  importedSources: number
  unchangedSources: number
  importedEntries: number
  importedTokens: number
}

export type NotificationChannelInput =
  | { kind: 'feishu'; name: string; webhookUrl: string }
  | { kind: 'qq_mail'; name: string; sender: string; authCode: string; recipient: string }

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

export const authenticationRequiredEvent = 'supermonitor:authentication-required'

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: 'same-origin',
    headers: {
      Accept: 'application/json',
      ...init?.headers,
    },
  })

  if (!response.ok) {
    if (response.status === 401 && path !== '/auth/session' && typeof window !== 'undefined') {
      window.dispatchEvent(new Event(authenticationRequiredEvent))
    }
    const payload = (await response.json().catch(() => ({}))) as APIErrorShape
    throw new APIError(
      payload.error?.message ?? `Request failed with status ${response.status}`,
      response.status,
      payload.error?.code ?? 'request_failed',
    )
  }

  if (response.status === 204) return undefined as T

  return (await response.json()) as T
}

export const api = {
  authStatus: () => request<{ required: boolean; authenticated: boolean }>('/auth/status'),
  login: (password: string, remember = true) => request<{ authenticated: boolean }>('/auth/session', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password, remember }),
  }),
  logout: () => request<{ authenticated: boolean }>('/auth/session', { method: 'DELETE' }),
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
  importCodexUsage: (id: string, sources: CodexUsageImportSource[]) => request<CodexUsageImportResult>(`/accounts/${encodeURIComponent(id)}/usage/codex-import`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ sources }),
  }),
  deleteAccount: (id: string) => request<void>(`/accounts/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  notificationChannels: () => request<{ items: NotificationChannel[] }>('/notifications/channels'),
  notificationPolicy: () => request<NotificationPolicy>('/notifications/policy'),
  createNotificationChannel: (input: NotificationChannelInput) => request<NotificationChannel>('/notifications/channels', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(input),
  }),
  deleteNotificationChannel: (id: string) => request<void>(`/notifications/channels/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  testNotificationChannel: (id: string) => request<{ status: string; message: string }>(`/notifications/channels/${encodeURIComponent(id)}/test`, { method: 'POST' }),
  evaluateNotifications: () => request<NotificationEvaluation>('/notifications/evaluate', { method: 'POST' }),
  activities: () => request<{ items: ActivityItem[]; warning?: string }>('/activities'),
  runActivity: (accountId: string, activityId: string) => request<ActivityItem>(`/activities/${encodeURIComponent(accountId)}/${encodeURIComponent(activityId)}`, { method: 'POST' }),
}
