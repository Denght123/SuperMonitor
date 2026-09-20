/** @vitest-environment jsdom */

import { act, type Dispatch, type SetStateAction, useState } from 'react'
import { createRoot, type Root } from 'react-dom/client'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { api, type DeviceLoginSession } from '../api/client'
import type { ToastContextValue } from './useToast'
import { useDeviceLoginPolling } from './useDeviceLoginPolling'

const pendingSession: DeviceLoginSession = {
  id: 'oauth-session-1',
  provider: 'qoder-cn',
  status: 'pending',
  verifyUrl: 'https://example.com/device',
  expiresAt: '2026-09-20T12:00:00Z',
  message: '等待授权',
}

type HarnessProps = {
  notify: ToastContextValue['notify']
  onConnected: () => void
  setBusy: Dispatch<SetStateAction<boolean>>
  setError: Dispatch<SetStateAction<string>>
}

function PollingHarness({ notify, onConnected, setBusy, setError }: HarnessProps) {
  const [session, setSession] = useState<DeviceLoginSession | null>(pendingSession)
  useDeviceLoginPolling({ session, providerName: 'Qoder CN', setSession, setBusy, setError, notify, onConnected })
  return <span data-session-status={session?.status}>{session?.message}</span>
}

describe('useDeviceLoginPolling', () => {
  let container: HTMLDivElement
  let root: Root

  beforeEach(() => {
    vi.useFakeTimers()
    ;(globalThis as typeof globalThis & { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true
    container = document.createElement('div')
    document.body.append(container)
    root = createRoot(container)
  })

  afterEach(() => {
    act(() => root.unmount())
    container.remove()
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  it('keeps one interval when a pending response replaces the session object', async () => {
    const intervalSpy = vi.spyOn(window, 'setInterval')
    const statusSpy = vi.spyOn(api, 'providerOAuthStatus').mockResolvedValue({ ...pendingSession, message: '仍在等待' })
    const notify = vi.fn(() => 'toast-1')
    const setBusy = vi.fn()
    const setError = vi.fn()
    const onConnected = vi.fn()

    act(() => root.render(<PollingHarness notify={notify} onConnected={onConnected} setBusy={setBusy} setError={setError} />))
    expect(intervalSpy).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    expect(container.textContent).toBe('仍在等待')
    expect(statusSpy).toHaveBeenCalledTimes(1)
    expect(intervalSpy).toHaveBeenCalledTimes(1)

    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })
    expect(statusSpy).toHaveBeenCalledTimes(2)
    expect(intervalSpy).toHaveBeenCalledTimes(1)
  })

  it('announces completion and closes the dialog after the existing delay', async () => {
    vi.spyOn(api, 'providerOAuthStatus').mockResolvedValue({ ...pendingSession, status: 'completed', message: '授权完成' })
    const notify = vi.fn(() => 'toast-1')
    const onConnected = vi.fn()
    const setBusy = vi.fn()
    const setError = vi.fn()

    act(() => root.render(<PollingHarness notify={notify} onConnected={onConnected} setBusy={setBusy} setError={setError} />))
    await act(async () => { await vi.advanceTimersByTimeAsync(2500) })

    expect(container.querySelector('[data-session-status]')?.getAttribute('data-session-status')).toBe('completed')
    expect(notify).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success', title: 'Qoder CN 授权成功' }))
    expect(onConnected).not.toHaveBeenCalled()

    act(() => vi.advanceTimersByTime(899))
    expect(onConnected).not.toHaveBeenCalled()
    act(() => vi.advanceTimersByTime(1))
    expect(onConnected).toHaveBeenCalledTimes(1)
  })
})
