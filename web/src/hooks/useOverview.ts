import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type Overview } from '../api/client'

type State = {
  data: Overview | null
  loading: boolean
  refreshing: boolean
  error: string | null
  streamStatus: 'connecting' | 'live' | 'offline'
  lastEvent: string | null
}

export function useOverview() {
  const [state, setState] = useState<State>({
    data: null,
    loading: true,
    refreshing: false,
    error: null,
    streamStatus: 'connecting',
    lastEvent: null,
  })
  const mounted = useRef(true)

  const load = useCallback(async (silent = false) => {
    if (!silent) {
      setState((current) => ({ ...current, loading: current.data === null, error: null }))
    }
    try {
      const data = await api.overview()
      if (mounted.current) {
        setState((current) => ({ ...current, data, loading: false, error: null }))
      }
    } catch (error) {
      if (mounted.current) {
        setState((current) => ({
          ...current,
          loading: false,
          error: error instanceof Error ? error.message : '无法加载监控总览',
        }))
      }
    }
  }, [])

  const refresh = useCallback(async () => {
    setState((current) => ({ ...current, refreshing: true, error: null }))
    try {
      await api.refresh()
      await load(true)
    } catch (error) {
      setState((current) => ({
        ...current,
        error: error instanceof Error ? error.message : '刷新失败',
      }))
    } finally {
      if (mounted.current) {
        setState((current) => ({ ...current, refreshing: false }))
      }
    }
  }, [load])

  useEffect(() => {
    mounted.current = true
    void load()
    const stream = new EventSource('/api/v1/events')
    stream.onopen = () => setState((current) => ({ ...current, streamStatus: 'live' }))
    stream.onerror = () => setState((current) => ({ ...current, streamStatus: 'offline' }))
    stream.addEventListener('update', (event) => {
      const payload = JSON.parse((event as MessageEvent<string>).data) as { message?: string }
      setState((current) => ({ ...current, lastEvent: payload.message ?? '数据已更新' }))
      void load(true)
    })
    return () => {
      mounted.current = false
      stream.close()
    }
  }, [load])

  return { ...state, refresh, retry: load }
}
