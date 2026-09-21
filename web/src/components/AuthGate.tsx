import { type FormEvent, type ReactNode, useCallback, useEffect, useRef, useState } from 'react'
import { AlertTriangle, CircleGauge, Eye, EyeOff, KeyRound, LoaderCircle, Server, ShieldCheck } from 'lucide-react'
import { api, authenticationRequiredEvent } from '../api/client'
import { AuthenticationContext } from '../hooks/useAuthentication'
import { useToast } from '../hooks/useToast'

type AuthState = 'checking' | 'authenticated' | 'required' | 'unavailable'

const minimumPasswordLength = 12

function normalizeConnectionAddress(raw: string): string | null {
  if (typeof window === 'undefined') return null
  const value = raw.trim()
  if (!value) return null
  const candidate = /^[a-z][a-z\d+.-]*:\/\//i.test(value) ? value : `${window.location.protocol}//${value}`
  try {
    const parsed = new URL(candidate)
    if (!['http:', 'https:'].includes(parsed.protocol) || parsed.username || parsed.password || (parsed.pathname !== '/' && parsed.pathname !== '')) return null
    if (parsed.search || parsed.hash) return null
    const hostname = parsed.hostname.replace(/^\[|\]$/g, '').toLowerCase()
    const privateIPv4 = /^(?:10\.|192\.168\.|172\.(?:1[6-9]|2\d|3[01])\.)/.test(hostname)
    const privateIPv6 = hostname === '::1' || /^(?:fc|fd|fe[89ab])/i.test(hostname)
    const trustedLocal = hostname === 'localhost' || hostname.endsWith('.local') || hostname.startsWith('127.') || privateIPv4 || privateIPv6
    if (parsed.protocol !== 'https:' && !trustedLocal) return null
    return parsed.origin
  } catch {
    return null
  }
}

function authenticationMessage(): string {
  if (typeof window === 'undefined') return ''
  const code = new URLSearchParams(window.location.search).get('auth_error')
  return ({
    invalid_request: '登录请求格式不正确，请重新填写。',
    invalid_address: '连接地址无效，请填写完整的 HTTP 或 HTTPS 地址。',
    invalid_password: '管理密码不正确。',
    rate_limited: '管理密码连续验证失败次数过多，请稍后再试。',
    session_failed: '暂时无法建立管理会话，请重试。',
  } as Record<string, string>)[code ?? ''] ?? ''
}

function clearAuthenticationQuery() {
  if (typeof window === 'undefined') return
  const url = new URL(window.location.href)
  if (!url.searchParams.has('auth') && !url.searchParams.has('auth_error')) return
  url.searchParams.delete('auth')
  url.searchParams.delete('auth_error')
  window.history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`)
}

export function AuthGate({ children }: { children: ReactNode }) {
  const { notify } = useToast()
  const [state, setState] = useState<AuthState>('checking')
  const [password, setPassword] = useState('')
  const [address, setAddress] = useState(() => typeof window === 'undefined' ? '' : window.location.origin)
  const [showPassword, setShowPassword] = useState(false)
  const [remember, setRemember] = useState(true)
  const [message, setMessage] = useState(authenticationMessage)
  const [submitting, setSubmitting] = useState(false)
  const [required, setRequired] = useState(false)
  const mounted = useRef(true)

  const check = useCallback(async () => {
    const authMessage = authenticationMessage()
    setState('checking')
    setMessage('')
    try {
      const status = await api.authStatus()
      if (mounted.current) {
        setRequired(status.required)
        setState(!status.required || status.authenticated ? 'authenticated' : 'required')
        if (status.required && !status.authenticated && authMessage) setMessage(authMessage)
        if (status.authenticated && new URLSearchParams(window.location.search).get('auth') === 'connected') {
          notify({ tone: 'success', title: '连接成功', message: `已连接 ${window.location.origin}，管理会话已建立。` })
        }
        clearAuthenticationQuery()
      }
    } catch (reason) {
      if (mounted.current) {
        setMessage(reason instanceof Error ? reason.message : '无法连接 SuperMonitor 服务')
        setState('unavailable')
      }
    }
  }, [notify])

  useEffect(() => {
    mounted.current = true
    void check()
    return () => { mounted.current = false }
  }, [check])

  useEffect(() => {
    const handleUnauthorized = () => {
      setRequired(true)
      setPassword('')
      setMessage('管理会话已过期，请重新输入管理密码。')
      setState('required')
    }
    window.addEventListener(authenticationRequiredEvent, handleUnauthorized)
    return () => window.removeEventListener(authenticationRequiredEvent, handleUnauthorized)
  }, [])

  const submit = (event: FormEvent) => {
    if (submitting) {
      event.preventDefault()
      return
    }
    const normalizedAddress = normalizeConnectionAddress(address)
    if (!normalizedAddress) {
      event.preventDefault()
      setMessage('请输入有效的 HTTP 或 HTTPS 连接地址。')
      return
    }
    if ([...password].length < minimumPasswordLength) {
      event.preventDefault()
      setMessage(`管理密码至少需要 ${minimumPasswordLength} 个字符。`)
      return
    }
    setSubmitting(true)
    setMessage('')
  }

  const logout = async () => {
    await api.logout()
    setRequired(true)
    setMessage('')
    setState('required')
  }
  const context = { required, logout }
  const passwordLength = [...password].length
  const connectionAddress = normalizeConnectionAddress(address)
  const formAction = connectionAddress ? `${connectionAddress}/api/v1/auth/connect` : '/api/v1/auth/connect'

  if (state === 'authenticated') return <AuthenticationContext.Provider value={context}>{children}</AuthenticationContext.Provider>

  return <main className="auth-shell">
    <section className="auth-console" aria-labelledby="auth-title" aria-busy={state === 'checking' || submitting}>
      <div className="auth-brand"><span><CircleGauge size={24} /></span><strong>SuperMonitor</strong></div>
      {state === 'checking' ? <div className="auth-state" role="status"><LoaderCircle className="spin" size={25} /><div><h1 id="auth-title">正在验证本机会话</h1><p>确认控制台访问权限后继续加载额度数据。</p></div></div> : null}
      {state === 'unavailable' ? <div className="auth-state error" role="alert"><AlertTriangle size={25} /><div><h1 id="auth-title">暂时无法连接服务</h1><p>{message}</p><button className="secondary-button" type="button" onClick={() => void check()}>重新连接</button></div></div> : null}
      {state === 'required' ? <form className="auth-form" method="post" action={formAction} onSubmit={submit}>
        <ShieldCheck size={28} />
        <div><h1 id="auth-title">连接管理服务</h1><p>填写 SuperMonitor 地址和管理密码。密码仅通过当前表单发送，并交换为 HttpOnly 会话。</p></div>
        <label htmlFor="connection-address">连接地址</label>
        <div className="auth-input"><Server size={18} /><input id="connection-address" type="text" inputMode="url" value={address} onChange={(event) => { setAddress(event.target.value); if (message) setMessage('') }} autoCapitalize="none" autoCorrect="off" spellCheck={false} required placeholder="https://monitor.example.com" aria-describedby="connection-address-hint" aria-invalid={!connectionAddress} /></div>
        <input type="hidden" name="address" value={connectionAddress ?? ''} />
        <p className="auth-hint" id="connection-address-hint">默认使用当前站点；公网地址必须使用 HTTPS，本机或私有局域网可以使用 HTTP。</p>
        <label htmlFor="admin-password">管理密码</label>
        <div className="auth-input"><KeyRound size={18} /><input id="admin-password" name="password" type={showPassword ? 'text' : 'password'} value={password} onChange={(event) => { setPassword(event.target.value); if (message) setMessage('') }} minLength={minimumPasswordLength} autoComplete="current-password" autoCapitalize="none" spellCheck={false} autoFocus required placeholder={`至少 ${minimumPasswordLength} 个字符`} aria-describedby="admin-password-hint" aria-invalid={Boolean(message)} /><button className="auth-reveal" type="button" onClick={() => setShowPassword((current) => !current)} aria-label={showPassword ? '隐藏管理密码' : '显示管理密码'} title={showPassword ? '隐藏管理密码' : '显示管理密码'}>{showPassword ? <EyeOff size={17} /> : <Eye size={17} />}</button></div>
        <p className={`auth-hint ${passwordLength >= minimumPasswordLength ? 'ready' : ''}`} id="admin-password-hint">{passwordLength === 0 ? `至少 ${minimumPasswordLength} 个字符，区分大小写。` : passwordLength < minimumPasswordLength ? `还需输入 ${minimumPasswordLength - passwordLength} 个字符。` : '密码长度符合要求。'}</p>
        <label className="auth-remember"><input type="checkbox" name="remember" value="1" checked={remember} onChange={(event) => setRemember(event.target.checked)} /><span><strong>保持登录 30 天</strong><small>仅在当前浏览器保存加密会话 Cookie，不保存管理密码。</small></span></label>
        {message ? <p className="auth-error" role="alert">{message}</p> : null}
        <button className="primary-button" type="submit" disabled={submitting || passwordLength < minimumPasswordLength || !connectionAddress}>{submitting ? <><LoaderCircle className="spin" size={18} />正在连接</> : '连接并进入控制台'}</button>
      </form> : null}
    </section>
  </main>
}
