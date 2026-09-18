import type { components } from './schema'

export type Overview = components['schemas']['Overview']
export type AccountSummary = components['schemas']['AccountSummary']
export type Alert = components['schemas']['Alert']
export type QuotaSignal = components['schemas']['QuotaSignal']
export type Provider = components['schemas']['Provider']

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
}
