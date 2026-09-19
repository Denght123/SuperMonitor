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
import { api, type AccountSummary, type ActivityItem, type Alert, type DeviceLoginSession, type Overview, type Provider, type QuotaSignal } from './api/client'
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
        <div className="sidebar-foot"><ShieldCheck size={18} /><span>凭据本机加密</span><small>v0.6.1</small></div>
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
  if (page === 'activities') return <ActivitiesPage onReload={onReload} />
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
  const displayValue = signal.kind === 'rate_window' && percent !== undefined ? `${Math.round(percent)}%` : quotaValue(signal.value, signal.unit)
  const ratio = signal.total ? `${quotaValue(signal.value, signal.unit)} / ${quotaValue(signal.total, signal.unit)}` : signal.source
  return <article className={`quota-progress tone-${tone}`}>
    <div className="quota-progress-head"><div><span>{signal.provider}</span><strong>{signal.label}</strong></div><b>{displayValue}</b></div>
    {percent !== undefined ? <div className="progress-track" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}><span style={{ transform: `scaleX(${Math.max(1, Math.min(100, percent)) / 100})` }} /></div> : null}
    <div className="quota-meta"><span>{ratio}</span>{deadline ? <span><Clock3 size={15} />{signal.resetAt ? '重置' : '到期'}：{formatDateTime(deadline)}（{relativeTime(deadline)}）</span> : <span>长期有效</span>}</div>
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
    <section className="section-block provider-section"><div className="section-header"><div><h2>添加平台账号</h2><p>每个平台独立添加；只有专属认证与真实额度字段完成验证后才开放连接。</p></div><label className="provider-search"><Search size={17} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索平台" /></label></div>{groups.map((group) => <div key={group} className="provider-group"><h3>{group}</h3><div className="provider-grid">{filtered.filter((provider) => provider.category === group).map((provider) => <article className={`provider-card ${provider.liveAuth ? '' : 'is-researching'}`} key={provider.id}><div className="provider-card-top"><ProviderLogo providerId={provider.id} name={provider.name} /><span className={`adapter-state ${provider.liveAuth ? 'live' : 'pending'}`}>{provider.liveAuth ? '可连接' : '接入验证中'}</span></div><h4>{provider.name}</h4><p>{provider.description}</p><div className="capability-row">{provider.capabilities.slice(0, 3).map((capability) => <span key={capability}>{capabilityLabel(capability)}</span>)}</div><button className={provider.liveAuth ? 'provider-action active' : 'provider-action'} onClick={() => provider.liveAuth && setSelected(provider)} disabled={!provider.liveAuth}>{provider.liveAuth ? <><Plus size={17} />添加账号</> : '专属接入尚未开放'}</button></article>)}</div></div>)}</section>
    {selected ? <ProviderConnectDialog provider={selected} onClose={() => setSelected(null)} onConnected={() => { setSelected(null); onReload() }} /> : null}
  </>
}

function ProviderConnectDialog({ provider, onClose, onConnected }: { provider: Provider; onClose: () => void; onConnected: () => void }) {
  const [alias, setAlias] = useState('')
  const [file, setFile] = useState<File | null>(null)
  const [secret, setSecret] = useState('')
	const [secret2, setSecret2] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [session, setSession] = useState<DeviceLoginSession | null>(null)
  const isCodex = provider.id === 'codex'
  const isWorkBuddy = provider.id === 'workbuddy-cn' || provider.id === 'workbuddy-global'
  const isQoder = provider.id === 'qoder-cn' || provider.id === 'qoder-global'
  const isCursor = provider.id === 'cursor'
  const isKiro = provider.id === 'kiro'
  const isFileImport = isCodex || isWorkBuddy || isQoder || isCursor || isKiro || provider.id === 'trae-cn' || provider.id === 'claude-code' || provider.id === 'gemini-cli'
  const isOAuth = isCodex || isWorkBuddy || isQoder || isCursor || isKiro || provider.id === 'trae-cn'
  const isSecret = ['bailian', 'coze-cn', 'deepseek', 'mimo', 'zhipu', 'tokenrhythm'].includes(provider.id)

  useEffect(() => {
    const listener = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [onClose])

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
      setBusy(false)
      if (!isWorkBuddy) window.open(next.verifyUrl, '_blank', 'noopener,noreferrer')
    } catch (reason) { setError(reason instanceof Error ? reason.message : '无法启动登录'); setBusy(false) }
  }
  const connectSecret = async () => {
    if (!secret.trim() || (provider.id === 'bailian' && !secret2.trim())) { setError(secretConfig(provider.id).emptyError); return }
    setBusy(true); setError('')
    try { await api.connectSecret(provider.id, alias, secret, secret2); setSecret(''); setSecret2(''); onConnected() } catch (reason) { setError(reason instanceof Error ? reason.message : '连接失败'); setBusy(false) }
  }
  const fileCopy = credentialFileCopy(provider.id)
  const secretCopy = secretConfig(provider.id)

  return <div className="dialog-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose() }}>
    <section className="connect-dialog" role="dialog" aria-modal="true" aria-labelledby="connect-title">
      <header><div className="connect-title-row"><ProviderLogo providerId={provider.id} name={provider.name} size={30} /><div><span className="adapter-state live">真实连接</span><h2 id="connect-title">连接 {provider.name}</h2><p>{provider.description}</p></div></div><button className="close-button" onClick={onClose} aria-label="关闭连接窗口"><X size={20} /></button></header>
      {session ? <div className="device-panel"><div className="qr-shell"><QRCodeSVG value={session.verifyUrl} size={180} bgColor="transparent" fgColor="currentColor" /></div><div><span className="field-label">{isCodex ? 'OpenAI 官方设备验证码' : `${provider.name} 官方登录`}</span>{session.userCode ? <strong className="device-code">{session.userCode}</strong> : null}<p>{isCodex ? '扫描二维码或打开官方页面，输入验证码完成登录。' : `打开或扫描官方页面，完成 ${provider.name} 账号授权。`} 页面会自动检测结果并读取真实额度。</p><a className="primary-button" href={session.verifyUrl} target="_blank" rel="noreferrer">打开官方登录页 <ExternalLink size={17} /></a><span className="login-status"><LoaderCircle className={session.status === 'pending' ? 'spin' : ''} size={17} />{session.message}</span>{error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}</div></div> : <div className="connect-body">
        <label><span className="field-label">账号备注（可选）</span><input value={alias} onChange={(event) => setAlias(event.target.value)} placeholder="例如：工作账号" /></label>
        {isFileImport ? <div className="auth-method"><div><FileJson size={22} /><span><strong>{fileCopy.title}</strong><small>{fileCopy.detail}</small></span></div><label className="file-picker"><Upload size={17} />{file ? file.name : '选择认证文件'}<input type="file" accept={fileCopy.accept} onChange={(event) => setFile(event.target.files?.[0] ?? null)} /></label><button className="primary-button" onClick={() => void importFile()} disabled={busy || !file}>{busy ? <LoaderCircle className="spin" size={17} /> : <Upload size={17} />}导入并读取真实额度</button></div> : null}
        {isOAuth ? <><div className="method-divider"><span>或</span></div><div className="auth-method device"><div><Globe2 size={22} /><span><strong>{isCodex ? 'OpenAI 官方设备登录' : `${provider.name} 官方登录`}</strong><small>授权完成后自动写入本机保险箱并读取实时额度</small></span></div><button className="secondary-button" onClick={() => void startOAuth()} disabled={busy}>{busy ? <LoaderCircle className="spin" size={17} /> : <ExternalLink size={17} />}{isCodex ? '获取登录验证码' : isWorkBuddy ? '生成登录二维码' : '打开官方登录'}</button></div></> : null}
        {isSecret ? <div className="auth-method"><div><KeyRound size={22} /><span><strong>{secretCopy.title}</strong><small>{secretCopy.detail}</small></span></div>{secretCopy.multiline ? <textarea className="secret-textarea" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder={secretCopy.placeholder} rows={5} /> : <input className="secret-input" type="password" autoComplete="off" value={secret} onChange={(event) => setSecret(event.target.value)} placeholder={secretCopy.placeholder} />}{provider.id === 'bailian' ? <input className="secret-input" type="password" autoComplete="off" value={secret2} onChange={(event) => setSecret2(event.target.value)} placeholder="AccessKey Secret" /> : null}{secretCopy.loginUrl ? <details className="login-guide"><summary>如何获取登录凭据</summary><ol><li><a href={secretCopy.loginUrl} target="_blank" rel="noreferrer">打开 {provider.name} 官方登录页 <ExternalLink size={14} /></a>，完成账号登录。</li><li>按 F12 打开开发者工具，进入“应用 / Application” → “Cookies”。</li><li>{provider.id === 'mimo' ? '选择 platform.xiaomimimo.com，复制完整 Cookie 请求头。' : provider.id === 'coze-cn' ? '选择 www.coze.cn，复制完整 Cookie 请求头；不要只复制某一个字段。' : '选择 tokenrhythm.studio，复制 tr_session；如有 tr_ref_device 一并复制。'}</li></ol></details> : null}<button className="primary-button" onClick={() => void connectSecret()} disabled={busy || !secret.trim() || (provider.id === 'bailian' && !secret2.trim())}>{busy ? <LoaderCircle className="spin" size={17} /> : <KeyRound size={17} />}{secretCopy.action}</button></div> : null}
        {error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}<p className="security-copy"><ShieldCheck size={17} />认证内容只发送到本机后端，并使用 AES-GCM 加密保存；页面不会回显 token、API Key 或 Cookie。</p>
      </div>}
    </section>
  </div>
}

function credentialFileCopy(providerId: string) {
  const copies: Record<string, { title: string; detail: string; accept: string }> = {
    codex: { title: '导入 Codex 认证文件', detail: '支持 auth.json、CPA 扁平格式与 Sub2API 常见包装结构；同一账号的 token 不会交叉混用。', accept: 'application/json,.json' },
    'workbuddy-cn': { title: '导入 WorkBuddy / CodeBuddy 凭据', detail: '支持官方工具或 WorkBuddy Switch 导出的 .info / JSON 文件。', accept: 'application/json,.json,.info' },
    'workbuddy-global': { title: '导入 WorkBuddy 国际版凭据', detail: '支持 WorkBuddy Switch 导出的 .info / JSON 文件。', accept: 'application/json,.json,.info' },
    'claude-code': { title: '导入 Claude Code OAuth 凭据', detail: '选择 Claude Code 的 .credentials.json，自动读取 5 小时与周限额。', accept: 'application/json,.json' },
    'gemini-cli': { title: '导入 Gemini CLI OAuth 凭据', detail: '选择 Gemini CLI 的 oauth_creds.json 读取各模型额度；过期后可重新导入，或在部署端配置 OAuth 客户端参数自动刷新。', accept: 'application/json,.json' },
		'qoder-cn': { title: '导入 Qoder CN 认证文件', detail: '支持 Qoder / CPA 导出的 dt-/drt- 或 jt-/jrt- JSON；也可以使用下方官方设备登录。', accept: 'application/json,.json' },
		'qoder-global': { title: '导入 Qoder 国际版认证文件', detail: '支持 Qoder / CPA 导出的认证 JSON；也可以使用下方官方设备登录。', accept: 'application/json,.json' },
		cursor: { title: '导入 Cursor 认证 JSON', detail: '支持 accessToken / refreshToken JSON；推荐使用下方 Cursor 官方网页登录。', accept: 'application/json,.json' },
		kiro: { title: '导入 Kiro 认证 JSON', detail: '支持 Kiro 官方缓存/账号工具导出的 accessToken、refreshToken 与 profileArn；推荐使用下方官方网页登录。', accept: 'application/json,.json' },
		'trae-cn': { title: '导入 TRAE CN 认证 JSON', detail: '支持 TRAE 账号工具或 CPA 插件导出的 accessToken/deviceId JSON；本机部署推荐使用下方官方网页登录。', accept: 'application/json,.json' },
  }
  return copies[providerId] ?? { title: '导入认证文件', detail: '导入该平台的本地认证文件。', accept: 'application/json,.json' }
}

function secretConfig(providerId: string) {
  const copies: Record<string, { title: string; detail: string; placeholder: string; action: string; emptyError: string; multiline?: boolean; loginUrl?: string }> = {
		bailian: { title: '阿里云 RAM 只读 AccessKey', detail: '调用阿里云官方 BSS QueryAccountBalance。请使用专用 RAM 用户并授予 AliyunBSSReadOnlyAccess；这是阿里云账户级余额，不是虚构的 Token Plan 数据。', placeholder: 'AccessKey ID（LTAI...）', action: '验证并读取阿里云余额', emptyError: '请同时填写 AccessKey ID 与 AccessKey Secret' },
		'coze-cn': { title: '扣子官网网页登录态', detail: '扣子目前没有面向额度监控的公开 OAuth scope；使用官网 Cookie 调用站点自身的 /credit/balance 接口读取积分。', placeholder: '粘贴 www.coze.cn 的完整 Cookie', action: '验证并读取扣子积分', emptyError: '请粘贴扣子官网 Cookie', multiline: true, loginUrl: 'https://www.coze.cn/' },
    deepseek: { title: 'DeepSeek API Key', detail: '调用官方 /user/balance 接口读取人民币账户余额。', placeholder: 'sk-...', action: '验证并读取余额', emptyError: '请输入 DeepSeek API Key' },
    zhipu: { title: '智谱开放平台 API Key', detail: '优先读取 Coding Plan 额度；普通开放平台 Key 会通过官方模型接口验证并显示可用模型数量，不会伪造额度。', placeholder: '粘贴 open.bigmodel.cn API Key', action: '验证 API Key 并读取信息', emptyError: '请输入智谱 API Key' },
    mimo: { title: '小米 MiMo 控制台 Cookie', detail: '仅用于读取小米 MiMo Token Plan；与华为基元律动完全独立。平台未提供可用于额度读取的 OAuth，因此提供官方登录跳转与逐步获取教程。', placeholder: '在 platform.xiaomimimo.com 登录后复制请求 Cookie', action: '验证并读取 Token Plan', emptyError: '请粘贴小米 MiMo 控制台 Cookie', multiline: true, loginUrl: 'https://platform.xiaomimimo.com/' },
    tokenrhythm: { title: '基元律动网页登录态', detail: '华为基元律动 tokenrhythm.studio；支持 sess_ 会话令牌或 tr_session / tr_ref_device Cookie。与小米 MiMo 完全独立。', placeholder: 'sess_...\n或 tr_session=sess_...; tr_ref_device=...', action: '验证并读取人民币余额', emptyError: '请粘贴基元律动 sess_ 会话令牌或 Cookie', multiline: true, loginUrl: 'https://tokenrhythm.studio/' },
  }
  return copies[providerId] ?? { title: '认证信息', detail: '用于读取真实额度。', placeholder: '', action: '验证并连接', emptyError: '请输入认证信息' }
}

function UsagePage({ data }: { data: Overview }) { return <><KPIBand data={data} /><div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div></> }
function ActivitiesPage({ onReload }: { onReload: () => void }) {
  const [items, setItems] = useState<ActivityItem[]>([])
  const [loading, setLoading] = useState(true)
  const [running, setRunning] = useState<string[]>([])
  const [error, setError] = useState('')
  const load = () => { setLoading(true); setError(''); void api.activities().then((payload) => setItems(payload.items)).catch((reason: Error) => setError(reason.message)).finally(() => setLoading(false)) }
  useEffect(load, [])
  const run = async (item: ActivityItem) => {
    setRunning((current) => [...current, item.accountId]); setError('')
    try {
      const next = await api.runActivity(item.accountId, item.id)
      setItems((current) => current.map((entry) => entry.accountId === item.accountId && entry.id === item.id ? next : entry))
      onReload()
    } catch (reason) { setError(reason instanceof Error ? reason.message : '活动执行失败') }
    finally { setRunning((current) => current.filter((id) => id !== item.accountId)) }
  }
  const available = items.filter((item) => item.status === 'available')
  const runAll = async () => { for (const item of available) await run(item) }
  return <section className="section-block"><div className="section-header"><div><h2>可执行活动</h2><p>只展示后端已通过官方接口确认存在的真实活动。</p></div><button className="primary-button" onClick={() => void runAll()} disabled={!available.length || running.length > 0}>{running.length ? <LoaderCircle className="spin" size={17} /> : <Zap size={17} />}全部签到</button></div>
    {error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}
    {loading ? <div className="activity-loading"><LoaderCircle className="spin" size={20} />正在读取平台活动状态</div> : items.length ? <div className="activity-grid">{items.map((item) => { const busy = running.includes(item.accountId); const completed = item.status === 'completed'; return <article className={`activity-card ${completed ? 'completed' : ''}`} key={`${item.accountId}-${item.id}`}><div className="activity-icon">{completed ? <Check size={20} /> : <Zap size={20} />}</div><div className="activity-copy"><span>{item.provider}</span><strong>{item.accountAlias} · {item.title}</strong><small>{item.description}</small></div><button className={completed ? 'secondary-button completed' : 'primary-button'} onClick={() => void run(item)} disabled={completed || busy}>{busy ? <LoaderCircle className="spin" size={17} /> : completed ? <Check size={17} /> : <Zap size={17} />}{completed ? '今日已完成' : '立即签到'}</button></article> })}</div> : <EmptyState title="当前账号池没有可执行活动" detail="Codex 等没有签到活动的平台不会出现在这里；接入支持活动的国内 WorkBuddy 账号后会自动显示。" />}
  </section>
}
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
function authMethodLabel(value?: string) { return ({ credential_import: '认证文件导入', device_code: '官方设备登录', oauth: '官方网页登录', oauth_qr: '官方二维码登录', api_key: 'API Key', access_key: 'RAM AccessKey', cookie: '控制台 Cookie', session_token: '网页登录态' } as Record<string, string>)[value ?? ''] ?? (value || '未记录') }
