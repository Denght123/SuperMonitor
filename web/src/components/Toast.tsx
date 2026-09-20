import { type ReactNode, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { AlertTriangle, Check, Info, X } from 'lucide-react'
import { ToastContext, type ToastInput, type ToastTone } from '../hooks/useToast'

type ToastItem = ToastInput & {
  id: string
  duration: number
}

const defaultDurations: Record<ToastTone, number> = {
  success: 4400,
  info: 4600,
  error: 6500,
}

let toastSequence = 0

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const timers = useRef(new Map<string, number>())

  const dismiss = useCallback((id: string) => {
    const timer = timers.current.get(id)
    if (timer !== undefined) window.clearTimeout(timer)
    timers.current.delete(id)
    setItems((current) => current.filter((item) => item.id !== id))
  }, [])

  const notify = useCallback((input: ToastInput) => {
    const id = `toast-${++toastSequence}`
    const duration = input.duration ?? defaultDurations[input.tone]
    const item: ToastItem = { ...input, id, duration }

    setItems((current) => {
      const next = [...current, item]
      next.slice(0, -4).forEach((removed) => {
        const timer = timers.current.get(removed.id)
        if (timer !== undefined) window.clearTimeout(timer)
        timers.current.delete(removed.id)
      })
      return next.slice(-4)
    })
    timers.current.set(id, window.setTimeout(() => dismiss(id), duration))
    return id
  }, [dismiss])

  useEffect(() => () => {
    timers.current.forEach((timer) => window.clearTimeout(timer))
    timers.current.clear()
  }, [])

  const value = useMemo(() => ({ notify, dismiss }), [dismiss, notify])

  return <ToastContext.Provider value={value}>
    {children}
    <section className="toast-viewport" aria-label="操作通知" aria-live="polite" aria-relevant="additions removals">
      {items.map((item) => <ToastCard key={item.id} item={item} onDismiss={dismiss} />)}
    </section>
  </ToastContext.Provider>
}

function ToastCard({ item, onDismiss }: { item: ToastItem; onDismiss: (id: string) => void }) {
  const Icon = item.tone === 'success' ? Check : item.tone === 'error' ? AlertTriangle : Info
  return <article className={`toast-card ${item.tone}`} role={item.tone === 'error' ? 'alert' : 'status'} aria-atomic="true">
    <span className="toast-icon"><Icon size={18} /></span>
    <span className="toast-copy"><strong>{item.title}</strong>{item.message ? <small>{item.message}</small> : null}</span>
    <button type="button" onClick={() => onDismiss(item.id)} aria-label={`关闭通知：${item.title}`}><X size={16} /></button>
    <i className="toast-timer" style={{ animationDuration: `${item.duration}ms` }} aria-hidden="true" />
  </article>
}
