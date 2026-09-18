import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity, AlertTriangle, Bell, Boxes, Check, ChevronRight, CircleGauge, Clock3,
  Database, ExternalLink, FileJson, Globe2, KeyRound, LayoutDashboard, LoaderCircle, Moon,
  Plus, RefreshCw, Search, Settings, ShieldCheck, Sun, Upload, X, Zap,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import {
  Bar, CartesianGrid, Cell, ComposedChart, Line, Pie, PieChart,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { api, type AccountSummary, type Alert, type DeviceLoginSession, type Overview, type Provider, type QuotaSignal } from './api/client'
import { useOverview } from './hooks/useOverview'
import { compactNumber, formatMetric, quotaValue, relativeTime } from './lib/format'
import { ProviderLogo } from './components/ProviderLogo'

type Page = 'overview' | 'accounts' | 'usage' | 'activities' | 'alerts' | 'settings'
type Theme = 'light' | 'dark' | 'system'

const navItems = [
  { id: 'overview' as Page, label: '总览', icon: LayoutDashboard },
  { id: 'accounts' as Page, label: '账号池', icon: Boxes },
  { id: 'usage' as Page, label: '用量统计', icon: Activity },
  { id: 'activities' as Page, label: '活动中心', icon: Zap },
  { id: 'alerts' as Page, label: '告警', icon: Bell },
  { id: 'settings' as Page, label: '设置', icon: Settings },
]

const pageTitles: Record<Page, { title: string; description: string }> = {
  overview: { title: '监控总览', description: '集中查看每个账号的原生额度窗口、余额和刷新状态。' },
  accounts: { title: '账号池', description: '每个平台独立接入，真实凭据只在本机加密保存。' },
  usage: { title: '用量统计', description: '按日期和模型观察 Token 消耗，不混合余额与积分。' },
  activities: { title: '活动中心', description: '集中执行已验证的签到与积分活动。' },
  alerts: { title: '告警中心', description: '额度、认证与刷新异常集中处理。' },
  settings: { title: '系统设置', description: '管理主题、轮询、安全与数据保留。' },
}

function initialTheme(): Theme {
  const saved = localStorage.getItem('supermonitor-theme')
  return saved === 'dark' || saved === 'system' || saved === 'light' ? saved : 'light'
}

export function App() {
  const [page, setPage] = useState<Page>('overview')
  const [theme, setTheme] = useState<Theme>(initialTheme)
  const [selectedAccount, setSelectedAccount] = useState<AccountSummary | null>(null)
  const { data, loading, refreshing, error, streamStatus, lastEvent, refresh, retry } = useOverview()

  useEffect(() => {
    const root = document.documentElement
    const media = matchMedia('(prefers-color-scheme: dark)')
    const apply = () => { root.dataset.theme = theme === 'system' ? (media.matches ? 'dark' : 'light') : theme }
    apply()
    localStorage.setItem('supermonitor-theme', theme)
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [theme])

  const changePage = (next: Page) => {
    if (next === page) return
    if (document.startViewTransition) document.startViewTransition(() => setPage(next))
    else setPage(next)
  }
  const cycleTheme = () => setTheme((value) => value === 'light' ? 'dark' : value === 'dark' ? 'system' : 'light')

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="主导航">
        <button className="brand" onClick={() => changePage('overview')}>
          <span className="brand-mark"><CircleGauge size={23} /></span>
          <span><strong>SuperMonitor</strong><small>AI quota console</small></span>
        </button>
        <nav>
          {navItems.map(({ id, label, icon: Icon }) => (
            <button key={id} className={page === id ? 'nav-item active' : 'nav-item'} onClick={() => changePage(id)} aria-current={page === id ? 'page' : undefined}>
              <Icon size={20} /><span>{label}</span>
              {id === 'alerts' && data?.alerts.length ? <b>{data.alerts.length}</b> : null}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot"><ShieldCheck size={18} /><span>凭据本机加密</span><small>v0.4.0</small></div>
      </aside>

      <main className="main-stage">
        <header className="topbar">
          <div className="connection-state"><i className={`connection-dot ${streamStatus}`} /><span>本地部署 · {streamStatus === 'live' ? '实时连接' : '正在连接'}</span></div>
          <div className="topbar-actions">
            <button className="icon-button" onClick={cycleTheme} title={`当前主题：${theme}`} aria-label="切换主题">
              {theme === 'light' ? <Sun size={20} /> : theme === 'dark' ? <Moon size={20} /> : <Settings size={20} />}
              <span>{theme === 'light' ? '浅色' : theme === 'dark' ? '深色' : '跟随系统'}</span>
            </button>
            <button className="refresh-button" onClick={() => void refresh()} disabled={refreshing}>
              <RefreshCw size={19} className={refreshing ? 'spin' : ''} />{refreshing ? '刷新中' : '刷新全部'}
            </button>
          </div>
        </header>

        <section className="page-heading">
          <div><h1>{pageTitles[page].title}</h1><p>{pageTitles[page].description}</p></div>
          <div className="data-clock"><Clock3 size={18} /><span>最近同步</span><strong>{data ? new Date(data.generatedAt).toLocaleTimeString('zh-CN', { hour12: false }) : '--:--:--'}</strong></div>
        </section>
        {error ? <div className="error-banner"><AlertTriangle size={20} /><span>{error}</span><button onClick={() => void retry()}>重试</button></div> : null}
        {lastEvent ? <div className="event-toast"><Check size={17} />{lastEvent}</div> : null}

        <div className="page-surface" key={page}>
          {loading || !data ? <DashboardSkeleton /> : (
            <PageContent page={page} data={data} onSelectAccount={setSelectedAccount} onNavigate={changePage} onReload={() => void retry(true)} />
          )}
        </div>
      </main>
      {selectedAccount ? <AccountDrawer account={selectedAccount} onClose={() => setSelectedAccount(null)} onReload={() => void retry(true)} /> : null}
    </div>
  )
}

function PageContent({ page, data, onSelectAccount, onNavigate, onReload }: { page: Page; data: Overview; onSelectAccount: (account: AccountSummary) => void; onNavigate: (page: Page) => void; onReload: () => void }) {
  if (page === 'overview') return <OverviewPage data={data} onSelectAccount={onSelectAccount} onNavigate={onNavigate} />
  if (page === 'accounts') return <AccountsPage accounts={data.accounts} onSelectAccount={onSelectAccount} onReload={onReload} />
  if (page === 'usage') return <UsagePage data={data} />
  if (page === 'activities') return <ActivitiesPage accounts={data.accounts} />
  if (page === 'alerts') return <AlertsPage alerts={data.alerts} />
  return <SettingsPage />
}

function OverviewPage({ data, onSelectAccount, onNavigate }: { data: Overview; onSelectAccount: (account: AccountSummary) => void; onNavigate: (page: Page) => void }) {
  return <>
    <KPIBand data={data} />
    <div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div>
    <section className="section-block">
      <div className="section-header"><div><h2>额度窗口</h2><p>颜色按剩余比例变化，时间为平台返回的精确重置或到期时间。</p></div></div>
      {data.quotaSignals.length ? <QuotaGroups signals={data.quotaSignals} /> : <EmptyState title="暂无真实额度" detail="连接账号后，这里会显示平台返回的余额、积分或限额窗口。" />}
    </section>
    <section className="section-block">
      <div className="section-header"><div><h2>账号信号</h2><p>只展示已经完成认证并成功读取额度的本地账号。</p></div><button className="text-button" onClick={() => onNavigate('accounts')}>管理账号池 <ChevronRight size={17} /></button></div>
      <ProviderAccountSections accounts={data.accounts.slice(0, 8)} onSelectAccount={onSelectAccount} />
    </section>
  </>
}

function KPIBand({ data }: { data: Overview }) {
  return <section className="kpi-band">{data.kpis.map((kpi) => <div className={`kpi-item tone-${kpi.tone}`} key={kpi.id}><span>{kpi.label}</span><strong>{formatMetric(kpi.value, kpi.unit)}</strong><small>{kpi.delta > 0 ? `较前期 +${kpi.delta}%` : kpi.unit}</small></div>)}<div className="kpi-context"><Database size={20} /><div><strong>计量口径隔离</strong><span>Token、余额、积分和订阅限额分别统计</span></div></div></section>
}

function TokenChart({ data }: { data: Overview }) {
  if (!data.tokenTrend.length) return <section className="instrument-panel token-panel"><div className="panel-header"><div><h2>30 天 Token 轨迹</h2><p>输入、输出、缓存与请求量</p></div></div><EmptyState title="暂无 Token 用量" detail="平台返回可记录的模型用量后会生成趋势图。" /></section>
  return <section className="instrument-panel token-panel"><div className="panel-header"><div><h2>30 天 Token 轨迹</h2><p>输入、输出、缓存与请求量</p></div><span className="source-badge">30 天</span></div><div className="chart-wrap"><ResponsiveContainer width="100%" height="100%"><ComposedChart data={data.tokenTrend} margin={{ top: 10, right: 6, left: -8, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false} /><XAxis dataKey="date" tickFormatter={(value: string) => value.slice(5)} tick={{ fill: 'var(--text-muted)', fontSize: 13 }} axisLine={false} tickLine={false} minTickGap={26} /><YAxis yAxisId="tokens" tickFormatter={compactNumber} tick={{ fill: 'var(--text-muted)', fontSize: 13 }} axisLine={false} tickLine={false} /><YAxis yAxisId="requests" hide orientation="right" /><Tooltip content={<TokenTooltip />} /><Bar yAxisId="tokens" dataKey="cacheTokens" stackId="tokens" fill="var(--chart-cache)" /><Bar yAxisId="tokens" dataKey="inputTokens" stackId="tokens" fill="var(--chart-input)" /><Bar yAxisId="tokens" dataKey="outputTokens" stackId="tokens" fill="var(--chart-output)" radius={[4, 4, 0, 0]} /><Line yAxisId="requests" dataKey="requests" stroke="var(--chart-line)" strokeWidth={2} dot={false} /></ComposedChart></ResponsiveContainer></div></section>
}

function TokenTooltip({ active, payload, label }: { active?: boolean; payload?: { name: string; value: number; color: string }[]; label?: string }) {
  if (!active || !payload) return null
  return <div className="chart-tooltip"><strong>{label}</strong>{payload.map((item) => <span key={item.name}><i style={{ background: item.color }} />{item.name}<b>{compactNumber(item.value)}</b></span>)}</div>
}

function ModelDonut({ data }: { data: Overview }) {
  const total = data.modelUsage.reduce((sum, item) => sum + item.tokens, 0)
  if (!data.modelUsage.length || total <= 0) return <section className="instrument-panel model-panel"><div className="panel-header"><div><h2>模型用量分布</h2><p>仅统计带模型字段的 Token</p></div></div><EmptyState title="暂无模型分布" detail="真实模型用量写入后会自动生成占比。" /></section>
  return <section className="instrument-panel model-panel"><div className="panel-header"><div><h2>模型用量分布</h2><p>仅统计带模型字段的 Token</p></div></div><div className="donut-wrap"><ResponsiveContainer width="100%" height={220}><PieChart><Pie data={data.modelUsage} dataKey="tokens" nameKey="model" innerRadius={65} outerRadius={92} paddingAngle={2} stroke="none">{data.modelUsage.map((entry) => <Cell key={entry.model} fill={entry.color} />)}</Pie><Tooltip formatter={(value) => compactNumber(Number(value))} /></PieChart></ResponsiveContainer><div className="donut-total"><span>30 天合计</span><strong>{compactNumber(total)}</strong><small>tokens</small></div></div><div className="model-list">{data.modelUsage.map((item) => <div key={item.model}><i style={{ background: item.color }} /><span>{item.model}</span><b>{((item.tokens / total) * 100).toFixed(1)}%</b></div>)}</div></section>
}

function QuotaProgress({ signal }: { signal: QuotaSignal }) {
  const percent = signal.remainingPercent ?? (signal.total ? signal.value / signal.total * 100 : undefined)
  const tone = percent === undefined ? signal.status : percent <= 15 ? 'critical' : percent <= 35 ? 'warning' : 'healthy'
  const deadline = signal.resetAt ?? signal.expiresAt
  return <article className={`quota-progress tone-${tone}`}>
    <div className="quota-progress-head"><div><span>{signal.provider}</span><strong>{signal.label}</strong></div><b>{percent === undefined ? quotaValue(signal.value, signal.unit) : `${Math.round(percent)}%`}</b></div>
    {percent !== undefined ? <div className="progress-track" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}><span style={{ transform: `scaleX(${Math.max(1, Math.min(100, percent)) / 100})` }} /></div> : null}
    <div className="quota-meta"><span>{signal.source}</span>{deadline ? <span><Clock3 size={15} />{signal.resetAt ? '重置' : '到期'}：{formatDateTime(deadline)}（{relativeTime(deadline)}）</span> : <span>长期有效</span>}</div>
  </article>
}

function QuotaGroups({ signals }: { signals: QuotaSignal[] }) {
  const groups = useMemo(() => {
    const result = new Map<string, QuotaSignal[]>()
    signals.forEach((signal) => result.set(signal.provider, [...(result.get(signal.provider) ?? []), signal]))
    return [...result.entries()]
  }, [signals])
  return <div className="quota-groups">{groups.map(([provider, items]) => <section className="quota-provider-group" key={provider}><div className="quota-provider-heading"><span>{provider}</span><small>{items.length} 个额度窗口</small></div><div className="quota-card-grid">{items.map((signal, index) => <QuotaProgress key={`${signal.id}-${index}`} signal={signal} />)}</div></section>)}</div>
}

function AccountGrid({ accounts, onSelectAccount }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void }) {
  if (!accounts.length) return <EmptyState title="还没有账号" detail="从下方平台列表添加你的第一个账号。" />
  return <div className="account-grid">{accounts.map((account) => <button className={`account-card status-${account.status}`} key={account.id} onClick={() => onSelectAccount(account)}><div className="account-card-head"><ProviderLogo providerId={account.providerId} name={account.provider} /><div><strong>{account.alias}</strong><span>{account.email || `${account.region} · ${account.plan || account.services[0]}`}</span></div><ChevronRight size={18} /></div><div className="account-tags"><em>{authMethodLabel(account.authMethod)}</em><em className="live">实时</em></div>{account.quotaWindows.length ? <div className="account-windows">{account.quotaWindows.slice(0, 3).map((signal) => <QuotaProgress key={signal.id} signal={signal} />)}{account.quotaWindows.length > 3 ? <span className="more-windows">另有 {account.quotaWindows.length - 3} 个额度窗口，点击查看</span> : null}</div> : <div className="metric-summary"><strong>{account.primaryMetric}</strong><span>{account.secondaryMetric}</span></div>}<div className="account-card-foot"><span>更新于 {relativeTime(account.lastRefreshedAt)}</span><span>{account.source}</span></div>{account.error ? <div className="inline-fault"><AlertTriangle size={16} />{account.error}</div> : null}</button>)}</div>
}

function ProviderAccountSections({ accounts, onSelectAccount }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void }) {
  if (!accounts.length) return <EmptyState title="还没有账号" detail="从下方平台列表添加你的第一个账号。" />
  const groups = new Map<string, AccountSummary[]>()
  accounts.forEach((account) => groups.set(account.providerId, [...(groups.get(account.providerId) ?? []), account]))
  return <div className="provider-account-sections">{[...groups.entries()].map(([providerId, items]) => <section className="provider-account-section" key={providerId}><header><div className="provider-section-title"><ProviderLogo providerId={providerId} name={items[0].provider} /><span><strong>{items[0].provider}</strong><small>{items.length} 个已连接账号</small></span></div><span className="adapter-state live">实时额度</span></header><AccountGrid accounts={items} onSelectAccount={onSelectAccount} /></section>)}</div>
}

function AccountsPage({ accounts, onSelectAccount, onReload }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void; onReload: () => void }) {
  const [providers, setProviders] = useState<Provider[]>([])
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Provider | null>(null)
  useEffect(() => { void api.providers().then((value) => setProviders(value.items)) }, [])
  const filtered = useMemo(() => providers.filter((provider) => `${provider.name}${provider.category}${provider.description}`.toLowerCase().includes(query.toLowerCase())), [providers, query])
  const groups = ['国内平台', '国际平台']
  return <>
    <section className="section-block"><div className="section-header"><div><h2>已连接账号</h2><p>按应用归类 · {accounts.length} 个真实账号</p></div></div><ProviderAccountSections accounts={accounts} onSelectAccount={onSelectAccount} /></section>
    <section className="section-block provider-section"><div className="section-header"><div><h2>添加平台账号</h2><p>每个平台独立添加；必须实际联通并解析额度后才会写入账号池。</p></div><label className="provider-search"><Search size={17} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索平台" /></label></div>{groups.map((group) => <div key={group} className="provider-group"><h3>{group}</h3><div className="provider-grid">{filtered.filter((provider) => provider.category === group).map((provider) => <article className="provider-card" key={provider.id}><div className="provider-card-top"><ProviderLogo providerId={provider.id} name={provider.name} /><span className="adapter-state live">可连接</span></div><h4>{provider.name}</h4><p>{provider.description}</p><div className="capability-row">{provider.capabilities.slice(0, 3).map((capability) => <span key={capability}>{capabilityLabel(capability)}</span>)}</div><button className="provider-action active" onClick={() => setSelected(provider)}><Plus size={17} />添加账号</button></article>)}</div></div>)}</section>
    {selected ? <ProviderConnectDialog provider={selected} onClose={() => setSelected(null)} onConnected={() => { setSelected(null); onReload() }} /> : null}
  </>
}

function ProviderConnectDialog({ provider, onClose, onConnected }: { provider: Provider; onClose: () => void; onConnected: () => void }) {
  const [alias, setAlias] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [secret, setSecret] = useState('')
  const [endpoint, setEndpoint] = useState('')
  const [method, setMethod] = useState<'GET' | 'POST'>('GET')
  const [authType, setAuthType] = useState<'bearer' | 'header' | 'cookie' | 'none'>('bearer')
  const [headerName, setHeaderName] = useState('Authorization')
  const [headerPrefix, setHeaderPrefix] = useState('')
  const [requestBody, setRequestBody] = useState('')
  const [valuePath, setValuePath] = useState('data.remaining')
  const [totalPath, setTotalPath] = useState('')
  const [resetAtPath, setResetAtPath] = useState('')
  const [expiresAtPath, setExpiresAtPath] = useState('')
  const [quotaLabel, setQuotaLabel] = useState('剩余额度')
  const [unit, setUnit] = useState('credits')
  const [kind, setKind] = useState('quota')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [session, setSession] = useState<DeviceLoginSession | null>(null)
  const isCodex = provider.id === 'codex'
  const isWorkBuddy = provider.id === 'workbuddy-cn' || provider.id === 'workbuddy-global'
  const isSecret = provider.id === 'deepseek' || provider.id === 'mimo'
  const isCustom = !isCodex && !isWorkBuddy && !isSecret

  useEffect(() => {
    if (!session || session.status !== 'pending') return
    const timer = window.setInterval(() => {
      const statusRequest = session.provider === 'codex' ? api.codexDeviceLoginStatus(session.id) : api.providerOAuthStatus(session.provider, session.id)
      void statusRequest.then((next) => {
        setSession(next)
        if (next.status === 'completed') { window.clearInterval(timer); window.setTimeout(onConnected, 900) }
        if (next.status === 'failed') { window.clearInterval(timer); setBusy(false); setError(next.message) }
      }).catch((reason: Error) => { window.clearInterval(timer); setBusy(false); setError(reason.message) })
    }, 2500)
    return () => window.clearInterval(timer)
  }, [session, onConnected])

  const importFile = async () => {
    if (!file) { setError('请先选择认证文件'); return }
    setBusy(true); setError('')
    try { await api.importCredential(provider.id, file, alias); setFile(null); onConnected() } catch (reason) { setError(reason instanceof Error ? reason.message : '导入失败'); setBusy(false) }
  }
  const startOAuth = async () => {
    setBusy(true); setError('')
    try {
      const next = isCodex ? await api.startCodexDeviceLogin(alias) : await api.startProviderOAuth(provider.id, alias)
      setSession(next)
      if (isCodex) window.open(next.verifyUrl, '_blank', 'noopener,noreferrer')
    } catch (reason) { setError(reason instanceof Error ? reason.message : '无法启动登录'); setBusy(false) }
  }
  const connectSecret = async () => {
    if (!secret.trim()) { setError(provider.id === 'deepseek' ? '请输入 DeepSeek API Key' : '请粘贴 MiMo 控制台 Cookie'); return }
    setBusy(true); setError('')
    try { await api.connectSecret(provider.id, alias, secret); setSecret(''); onConnected() } catch (reason) { setSecret(''); setError(reason instanceof Error ? reason.message : '连接失败'); setBusy(false) }
  }
  const connectCustom = async () => {
    if (!endpoint.trim() || !valuePath.trim()) { setError('请填写额度接口 URL 和剩余额度字段路径'); return }
    if (authType !== 'none' && !secret.trim()) { setError('请填写该接口需要的认证内容'); return }
    setBusy(true); setError('')
    try {
      await api.connectCustomQuota(provider.id, {
        alias: alias.trim(), endpoint: endpoint.trim(), method, authType,
        headerName: authType === 'header' ? headerName.trim() : undefined,
        headerPrefix: authType === 'header' ? headerPrefix : undefined,
        secret: authType === 'none' ? undefined : secret,
        requestBody: method === 'POST' ? requestBody.trim() : undefined,
        valuePath: valuePath.trim(), totalPath: totalPath.trim() || undefined,
        resetAtPath: resetAtPath.trim() || undefined, expiresAtPath: expiresAtPath.trim() || undefined,
        label: quotaLabel.trim() || '剩余额度', unit: unit.trim() || 'credits', kind: kind.trim() || 'quota',
      })
      setSecret(''); onConnected()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '接口验证失败'); setBusy(false) }
  }

  return <div className="dialog-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onClose() }}>
    <section className="connect-dialog" role="dialog" aria-modal="true" aria-labelledby="connect-title">
      <header><div className="connect-title-row"><ProviderLogo providerId={provider.id} name={provider.name} size={30} /><div><span className="adapter-state live">真实连接</span><h2 id="connect-title">连接 {provider.name}</h2><p>{provider.description}</p></div></div><button className="close-button" onClick={onClose} disabled={busy}><X size={20} /></button></header>
      {session ? <div className="device-panel"><div className="qr-shell"><QRCodeSVG value={session.verifyUrl} size={180} bgColor="transparent" fgColor="currentColor" /></div><div><span className="field-label">{isCodex ? 'OpenAI 官方设备验证码' : '官方扫码登录'}</span>{session.userCode ? <strong className="device-code">{session.userCode}</strong> : null}<p>{isCodex ? '扫描二维码或打开官方页面，输入验证码完成登录。' : '用手机扫描左侧二维码，在 WorkBuddy / CodeBuddy 官方页面完成授权。'} 页面会自动检测结果并读取真实额度。</p><a className="primary-button" href={session.verifyUrl} target="_blank" rel="noreferrer">打开官方登录页 <ExternalLink size={17} /></a><span className="login-status"><LoaderCircle className={session.status === 'pending' ? 'spin' : ''} size={17} />{session.message}</span>{error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}</div></div> : <div className="connect-body">
        <label><span className="field-label">账号备注（可选）</span><input value={alias} onChange={(event) => setAlias(event.target.value)} placeholder="例如：工作账号" /></label>
        {(isCodex || isWorkBuddy) ? <><div className="auth-method"><div><FileJson size={22} /><span><strong>导入认证文件</strong><small>{isCodex ? '支持 Codex auth.json、CPA 与 Sub2API 常见导出结构' : '支持 WorkBuddy / CodeBuddy 导出的 .info 或 JSON 凭据'}</small></span></div><label className="file-picker"><Upload size={17} />{file ? file.name : '选择认证文件'}<input type="file" accept={isCodex ? 'application/json,.json' : 'application/json,.json,.info'} onChange={(event) => setFile(event.target.files?.[0] ?? null)} /></label><button className="primary-button" onClick={() => void importFile()} disabled={busy || !file}>{busy ? <LoaderCircle className="spin" size={17} /> : <Upload size={17} />}导入并读取额度</button></div><div className="method-divider"><span>或</span></div><div className="auth-method device"><div><Globe2 size={22} /><span><strong>{isCodex ? 'OpenAI 官方设备登录' : '官方二维码登录'}</strong><small>授权完成后自动写入本机保险箱并读取实时额度</small></span></div><button className="secondary-button" onClick={() => void startOAuth()} disabled={busy}>{busy ? <LoaderCircle className="spin" size={17} /> : <ExternalLink size={17} />}{isCodex ? '获取登录验证码' : '生成登录二维码'}</button></div></> : null}
        {isSecret ? <div className="auth-method"><div><KeyRound size={22} /><span><strong>{provider.id === 'deepseek' ? 'DeepSeek API Key' : 'MiMo 控制台 Cookie'}</strong><small>{provider.id === 'deepseek' ? '调用官方 /user/balance 接口读取账户余额' : '读取 Token Plan 月度额度与周期'}</small></span></div>{provider.id === 'deepseek' ? <input className="secret-input" type="password" autoComplete="off" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder="sk-..." /> : <textarea className="secret-textarea" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder="在 platform.xiaomimimo.com 登录后，从浏览器请求头复制 Cookie" rows={5} />}<button className="primary-button" onClick={() => void connectSecret()} disabled={busy || !secret.trim()}>{busy ? <LoaderCircle className="spin" size={17} /> : <KeyRound size={17} />}验证并读取额度</button></div> : null}
        {isCustom ? <div className="auth-method custom-endpoint"><div><Globe2 size={22} /><span><strong>真实额度接口</strong><small>填写该平台的实际接口与 JSON 字段映射；联通、鉴权和字段解析全部成功后才保存。</small></span></div><div className="form-grid"><label className="field-span-2"><span className="field-label">额度接口 URL</span><input value={endpoint} onChange={(event) => setEndpoint(event.target.value)} placeholder="https://example.com/api/quota" /></label><label><span className="field-label">请求方式</span><select value={method} onChange={(event) => setMethod(event.target.value as 'GET' | 'POST')}><option>GET</option><option>POST</option></select></label><label><span className="field-label">认证方式</span><select value={authType} onChange={(event) => setAuthType(event.target.value as typeof authType)}><option value="bearer">Bearer Token</option><option value="header">自定义 Header</option><option value="cookie">Cookie</option><option value="none">无需认证</option></select></label>{authType === 'header' ? <><label><span className="field-label">Header 名称</span><input value={headerName} onChange={(event) => setHeaderName(event.target.value)} placeholder="X-API-Key" /></label><label><span className="field-label">值前缀（可空）</span><input value={headerPrefix} onChange={(event) => setHeaderPrefix(event.target.value)} placeholder="Bearer " /></label></> : null}{authType !== 'none' ? <label className="field-span-2"><span className="field-label">认证内容</span><input type="password" autoComplete="off" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder={authType === 'cookie' ? 'name=value; ...' : 'Token / API Key'} /></label> : null}{method === 'POST' ? <label className="field-span-2"><span className="field-label">JSON 请求体（可空）</span><textarea value={requestBody} onChange={(event) => setRequestBody(event.target.value)} placeholder="{}" rows={3} /></label> : null}<label><span className="field-label">剩余额度路径</span><input value={valuePath} onChange={(event) => setValuePath(event.target.value)} placeholder="data.remaining" /></label><label><span className="field-label">总额度路径（可空）</span><input value={totalPath} onChange={(event) => setTotalPath(event.target.value)} placeholder="data.total" /></label><label><span className="field-label">重置时间路径（可空）</span><input value={resetAtPath} onChange={(event) => setResetAtPath(event.target.value)} placeholder="data.reset_at" /></label><label><span className="field-label">到期时间路径（可空）</span><input value={expiresAtPath} onChange={(event) => setExpiresAtPath(event.target.value)} placeholder="data.expires_at" /></label><label><span className="field-label">额度名称</span><input value={quotaLabel} onChange={(event) => setQuotaLabel(event.target.value)} /></label><label><span className="field-label">单位</span><input value={unit} onChange={(event) => setUnit(event.target.value)} placeholder="credits / CNY / tokens" /></label><label className="field-span-2"><span className="field-label">类型</span><select value={kind} onChange={(event) => setKind(event.target.value)}><option value="quota">额度</option><option value="credits">积分</option><option value="balance">余额</option><option value="token_plan">Token Plan</option><option value="rate_window">限额窗口</option></select></label></div><button className="primary-button" onClick={() => void connectCustom()} disabled={busy}>{busy ? <LoaderCircle className="spin" size={17} /> : <Globe2 size={17} />}联通、验证并添加</button></div> : null}
        {error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}<p className="security-copy"><ShieldCheck size={17} />认证内容只发送到本机后端，并使用 AES-GCM 加密保存；页面不会回显 token、API Key 或 Cookie。</p>
      </div>}
    </section>
  </div>
}

function UsagePage({ data }: { data: Overview }) { return <><KPIBand data={data} /><div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div></> }
function ActivitiesPage({ accounts }: { accounts: AccountSummary[] }) { const supported = accounts.filter((account) => account.services.some((service) => /Buddy/i.test(service))); return <section className="section-block"><div className="section-header"><div><h2>可执行活动</h2><p>真实活动接口验证完成后才开放批量签到。</p></div><button className="primary-button" disabled>全部签到</button></div>{supported.map((account) => <div className="activity-row" key={account.id}><div><Zap size={20} /><span><strong>{account.alias}</strong><small>{account.provider} · 等待真实活动适配器</small></span></div><button className="secondary-button" disabled>立即签到</button></div>)}</section> }
function AlertsPage({ alerts }: { alerts: Alert[] }) { return <section className="section-block alert-list"><div className="section-header"><div><h2>未解决告警</h2><p>同一故障自动去重，恢复后保留记录。</p></div></div>{alerts.map((alert) => <article key={alert.id} className={`alert-item severity-${alert.severity}`}><AlertTriangle size={22} /><div><span>{alert.provider} · {relativeTime(alert.createdAt)}</span><strong>{alert.title}</strong><p>{alert.message}</p><small>建议：{alert.recovery}</small></div></article>)}</section> }
function SettingsPage() { return <div className="settings-grid"><SettingBlock icon={Sun} title="默认浅色主题" detail="可切换深色或跟随系统，选择会保存在浏览器。" /><SettingBlock icon={ShieldCheck} title="本机凭据保险箱" detail="AES-GCM 加密，密钥与数据库仅保存在部署机器。" /><SettingBlock icon={RefreshCw} title="额度轮询" detail="当前默认 10 分钟刷新，也可随时手动刷新。" /><SettingBlock icon={Database} title="数据隔离" detail="OAuth 文件、数据库、日志与备份均已加入忽略规则。" /></div> }
function SettingBlock({ icon: Icon, title, detail }: { icon: typeof Settings; title: string; detail: string }) { return <article className="setting-block"><Icon size={24} /><span><strong>{title}</strong><small>{detail}</small></span></article> }

function AccountDrawer({ account, onClose, onReload }: { account: AccountSummary; onClose: () => void; onReload: () => void }) {
  const ref = useRef<HTMLElement>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  useEffect(() => { const listener = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }; window.addEventListener('keydown', listener); ref.current?.focus(); return () => window.removeEventListener('keydown', listener) }, [onClose])
  const refreshAccount = async () => { setRefreshing(true); setError(''); try { await api.refreshAccount(account.id); onReload(); onClose() } catch (reason) { setError(reason instanceof Error ? reason.message : '刷新失败'); setRefreshing(false) } }
  return <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}><aside className="account-drawer" ref={ref} tabIndex={-1}><header><div><span>实时账号</span><h2>{account.alias}</h2><p>{account.email || account.provider} · {account.plan || account.region}</p></div><button className="close-button" onClick={onClose}><X size={22} /></button></header><div className="drawer-content"><div className="detail-row"><span>认证方式</span><strong>{authMethodLabel(account.authMethod)}</strong></div><div className="detail-row"><span>数据来源</span><strong>{account.source}</strong></div><div className="detail-row"><span>最近刷新</span><strong>{formatDateTime(account.lastRefreshedAt)}</strong></div><div className="drawer-quotas">{account.quotaWindows.length ? account.quotaWindows.map((signal) => <QuotaProgress key={signal.id} signal={signal} />) : <EmptyState title={account.primaryMetric} detail={account.secondaryMetric} />}</div>{error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}</div><footer><button className="primary-button" onClick={() => void refreshAccount()} disabled={refreshing}>{refreshing ? <LoaderCircle className="spin" size={18} /> : <RefreshCw size={18} />}立即读取真实额度</button></footer></aside></div>
}

function EmptyState({ title, detail }: { title: string; detail: string }) { return <div className="empty-state"><Boxes size={28} /><strong>{title}</strong><span>{detail}</span></div> }
function DashboardSkeleton() { return <div className="skeleton-grid">{Array.from({ length: 8 }).map((_, index) => <i key={index} />)}</div> }
function formatDateTime(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function capabilityLabel(value: string) { return ({ quota: '额度', usage: '用量', credits: '积分', balance: '余额', token_plan: 'Token Plan', checkin: '签到' } as Record<string, string>)[value] ?? value }
function authMethodLabel(value?: string) { return ({ credential_import: '认证文件导入', device_code: '官方设备登录', oauth_qr: '官方二维码登录', api_key: 'API Key', cookie: '控制台 Cookie' } as Record<string, string>)[value ?? ''] ?? (value || '未记录') }
