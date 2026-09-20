import { type FormEvent, type ReactNode, useEffect, useRef, useState } from 'react'
import { AlertTriangle, CircleGauge, KeyRound, LoaderCircle, ShieldCheck } from 'lucide-react'
import { api, authenticationRequiredEvent } from '../api/client'
import { AuthenticationContext } from '../hooks/useAuthentication'
import { useToast } from '../hooks/useToast'

type AuthState = 'checking' | 'authenticated' | 'required' | 'unavailable'

export function AuthGate({ children }: { children: ReactNode }) {
  const { notify } = useToast()
  const [state, setState] = useState<AuthState>('checking')
  const [token, setToken] = useState('')
  const [message, setMessage] = useState('')
  const [submitting, setSubmitting] = useState(false)
  const [required, setRequired] = useState(false)
  const mounted = useRef(true)

  const check = async () => {
    setState('checking')
    setMessage('')
    try {
      const status = await api.authStatus()
      if (mounted.current) {
        setRequired(status.required)
        setState(!status.required || status.authenticated ? 'authenticated' : 'required')
      }
    } catch (reason) {
      if (mounted.current) {
        setMessage(reason instanceof Error ? reason.message : '无法连接 SuperMonitor 服务')
        setState('unavailable')
      }
    }
  }

  useEffect(() => {
    mounted.current = true
    void check()
    return () => { mounted.current = false }
  }, [])

  useEffect(() => {
    const handleUnauthorized = () => {
      setRequired(true)
      setToken('')
      setMessage('管理会话已过期，请重新输入令牌。')
      setState('required')
    }
    window.addEventListener(authenticationRequiredEvent, handleUnauthorized)
    return () => window.removeEventListener(authenticationRequiredEvent, handleUnauthorized)
  }, [])

  const submit = async (event: FormEvent) => {
    event.preventDefault()
    if (submitting) return
    if ([...token.trim()].length < 24) {
      setMessage('管理员令牌至少需要 24 个字符。')
      return
    }
    setSubmitting(true)
    setMessage('')
    try {
      await api.login(token.trim())
      if (mounted.current) {
        setToken('')
        setState('authenticated')
        notify({ tone: 'success', title: '管理员验证成功', message: '已建立受保护的本浏览器管理会话。' })
      }
    } catch (reason) {
      if (mounted.current) {
        const nextMessage = reason instanceof Error ? reason.message : '管理员令牌验证失败'
        setMessage(nextMessage)
        notify({ tone: 'error', title: '管理员验证失败', message: nextMessage })
      }
    } finally {
      if (mounted.current) setSubmitting(false)
    }
  }

  const logout = async () => {
    await api.logout()
    setRequired(true)
    setMessage('')
    setState('required')
  }
  const context = { required, logout }
  const tokenLength = [...token.trim()].length

  if (state === 'authenticated') return <AuthenticationContext.Provider value={context}>{children}</AuthenticationContext.Provider>

  return <main className="auth-shell">
    <section className="auth-console" aria-labelledby="auth-title" aria-busy={state === 'checking' || submitting}>
      <div className="auth-brand"><span><CircleGauge size={24} /></span><strong>SuperMonitor</strong></div>
      {state === 'checking' ? <div className="auth-state" role="status"><LoaderCircle className="spin" size={25} /><div><h1 id="auth-title">正在验证本机会话</h1><p>确认控制台访问权限后继续加载额度数据。</p></div></div> : null}
      {state === 'unavailable' ? <div className="auth-state error" role="alert"><AlertTriangle size={25} /><div><h1 id="auth-title">暂时无法连接服务</h1><p>{message}</p><button className="secondary-button" type="button" onClick={() => void check()}>重新连接</button></div></div> : null}
      {state === 'required' ? <form className="auth-form" onSubmit={(event) => void submit(event)}>
        <ShieldCheck size={28} />
        <div><h1 id="auth-title">管理员验证</h1><p>输入部署时配置的管理员令牌。令牌仅用于建立本浏览器的 HttpOnly 会话，不会写入本地存储。</p></div>
        <label htmlFor="admin-token">管理员令牌</label>
        <div className="auth-input"><KeyRound size={18} /><input id="admin-token" type="password" value={token} onChange={(event) => { setToken(event.target.value); if (message) setMessage('') }} minLength={24} autoComplete="current-password" autoCapitalize="none" spellCheck={false} autoFocus required placeholder="至少 24 个字符" aria-describedby="admin-token-hint" aria-invalid={Boolean(message)} /></div>
        <p className={`auth-hint ${tokenLength >= 24 ? 'ready' : ''}`} id="admin-token-hint">{tokenLength === 0 ? '至少 24 个字符，区分大小写。' : tokenLength < 24 ? `还需输入 ${24 - tokenLength} 个字符。` : '长度符合要求，可以开始验证。'}</p>
        {message ? <p className="auth-error" role="alert">{message}</p> : null}
        <button className="primary-button" type="submit" disabled={submitting || tokenLength < 24}>{submitting ? <><LoaderCircle className="spin" size={18} />正在验证</> : '进入控制台'}</button>
      </form> : null}
    </section>
  </main>
}
