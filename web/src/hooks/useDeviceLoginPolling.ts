import { type Dispatch, type SetStateAction, useEffect, useRef } from 'react'

import { api, type DeviceLoginSession } from '../api/client'
import type { ToastContextValue } from './useToast'

type DeviceLoginPollingOptions = {
  session: DeviceLoginSession | null
  providerName: string
  setSession: Dispatch<SetStateAction<DeviceLoginSession | null>>
  setBusy: Dispatch<SetStateAction<boolean>>
  setError: Dispatch<SetStateAction<string>>
  notify: ToastContextValue['notify']
  onConnected: () => void
}

const pollIntervalMs = 2500
const completionDelayMs = 900

export function useDeviceLoginPolling({
  session,
  providerName,
  setSession,
  setBusy,
  setError,
  notify,
  onConnected,
}: DeviceLoginPollingOptions) {
  const connectedHandler = useRef(onConnected)

  useEffect(() => {
    connectedHandler.current = onConnected
  }, [onConnected])

  const sessionId = session?.id
  const sessionProvider = session?.provider
  const sessionStatus = session?.status

  useEffect(() => {
    if (!sessionId || !sessionProvider || sessionStatus !== 'pending') return

    let active = true
    let requestInFlight = false
    const timer = window.setInterval(() => {
      if (requestInFlight) return
      requestInFlight = true

      const statusRequest = sessionProvider === 'codex'
        ? api.codexDeviceLoginStatus(sessionId)
        : api.providerOAuthStatus(sessionProvider, sessionId)

      void statusRequest.then((next) => {
        if (!active) return
        setSession(next)
        if (next.status === 'completed') {
          window.clearInterval(timer)
          notify({ tone: 'success', title: `${providerName} 授权成功`, message: '账号已加入账号池，真实额度正在同步。' })
        } else if (next.status === 'failed') {
          window.clearInterval(timer)
          setBusy(false)
          setError(next.message)
          notify({ tone: 'error', title: `${providerName} 授权失败`, message: next.message })
        }
      }).catch((reason: unknown) => {
        if (!active) return
        window.clearInterval(timer)
        const message = reason instanceof Error ? reason.message : '登录状态读取失败'
        setBusy(false)
        setError(message)
        notify({ tone: 'error', title: `${providerName} 登录状态读取失败`, message })
      }).finally(() => {
        requestInFlight = false
      })
    }, pollIntervalMs)

    return () => {
      active = false
      window.clearInterval(timer)
    }
  }, [notify, providerName, sessionId, sessionProvider, sessionStatus, setBusy, setError, setSession])

  useEffect(() => {
    if (!sessionId || sessionStatus !== 'completed') return
    const timer = window.setTimeout(() => connectedHandler.current(), completionDelayMs)
    return () => window.clearTimeout(timer)
  }, [sessionId, sessionStatus])
}
