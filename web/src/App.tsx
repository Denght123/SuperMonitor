import { type FormEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity, AlertTriangle, ArrowDown, ArrowUp, Bell, Bot, Boxes, Check, ChevronRight, CircleGauge, Clock3,
  Database, ExternalLink, FileJson, Globe2, KeyRound, LayoutDashboard, LoaderCircle, Moon,
  LogOut, Mail, Plus, RefreshCw, ScanSearch, Search, Send, Settings, ShieldCheck, Sun, Trash2, Upload, X, Zap,
} from 'lucide-react'
import { QRCodeSVG } from 'qrcode.react'
import {
  Bar, CartesianGrid, Cell, ComposedChart, Line, Pie, PieChart,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from 'recharts'
import { api, type AccountSummary, type ActivityItem, type Alert, type DeviceLoginSession, type NotificationChannel, type NotificationChannelKind, type NotificationEvaluation, type NotificationPolicy, type Overview, type Provider, type QuotaSignal } from './api/client'
import { useOverview } from './hooks/useOverview'
import { compactNumber, formatMetric, quotaValue, relativeTime } from './lib/format'
import { filterUsageByRange, usageRangeDescription, usageRangeOptions, type UsageRange } from './lib/usageRange'
import { ProviderLogo } from './components/ProviderLogo'
import { useDeviceLoginPolling } from './hooks/useDeviceLoginPolling'
import { useToast } from './hooks/useToast'
import { providerShortName } from './lib/providerNames'
import { moveProviderInOrder, normalizeProviderOrder, readProviderOrder, sortProviderEntries, writeProviderOrder, type ProviderOrderDirection } from './lib/providerOrder'
import { useAuthentication } from './hooks/useAuthentication'
import { parseCodexUsageFiles } from './lib/codexUsageImport'

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
  settings: { title: '系统设置', description: '连接通知渠道，查看额度、重置与活动提醒策略。' },
}

function initialTheme(): Theme {
  const saved = localStorage.getItem('supermonitor-theme')
  return saved === 'dark' || saved === 'system' || saved === 'light' ? saved : 'light'
}

export function App() {
  const { notify } = useToast()
  const authentication = useAuthentication()
  const [page, setPage] = useState<Page>('overview')
  const [theme, setTheme] = useState<Theme>(initialTheme)
  const [selectedAccount, setSelectedAccount] = useState<AccountSummary | null>(null)
  const [providerOrder, setProviderOrder] = useState(readProviderOrder)
  const { data, loading, refreshing, error, streamStatus, refresh, retry } = useOverview()
  const visibleProviderIds = useMemo(() => data ? [...new Set(data.accounts.map((account) => account.providerId))] : [], [data])

  useEffect(() => {
    const root = document.documentElement
    const media = matchMedia('(prefers-color-scheme: dark)')
    const apply = () => { root.dataset.theme = theme === 'system' ? (media.matches ? 'dark' : 'light') : theme }
    apply()
    localStorage.setItem('supermonitor-theme', theme)
    media.addEventListener('change', apply)
    return () => media.removeEventListener('change', apply)
  }, [theme])

  useEffect(() => {
    setProviderOrder((current) => {
      const next = normalizeProviderOrder(current, visibleProviderIds)
      if (sameStringArray(current, next)) return current
      writeProviderOrder(next)
      return next
    })
  }, [visibleProviderIds])

  const changePage = (next: Page) => {
    if (next === page) return
    if (document.startViewTransition) document.startViewTransition(() => setPage(next))
    else setPage(next)
  }
  const cycleTheme = () => setTheme((value) => value === 'light' ? 'dark' : value === 'dark' ? 'system' : 'light')
  const refreshAll = async () => {
    try {
      const result = await refresh()
      notify({ tone: 'success', title: '全部账号刷新成功', message: result.message || '最新额度和活动状态已经写入总览。' })
    } catch (reason) {
      notify({ tone: 'error', title: '刷新全部失败', message: reason instanceof Error ? reason.message : '请检查网络与平台认证后重试。' })
    }
  }
  const logout = async () => {
    try {
      await authentication.logout()
      notify({ tone: 'info', title: '已退出管理会话', message: '再次进入控制台时需要重新输入管理密码。' })
    } catch (reason) {
      notify({ tone: 'error', title: '退出失败', message: reason instanceof Error ? reason.message : '请稍后重试。' })
    }
  }
  const moveProvider = useCallback((providerId: string, direction: ProviderOrderDirection, providerName: string) => {
    setProviderOrder((current) => {
      const next = moveProviderInOrder(current, visibleProviderIds, providerId, direction)
      writeProviderOrder(next)
      return next
    })
    notify({ tone: 'info', title: '显示顺序已保存', message: `${providerName} 已${direction === 'up' ? '上移' : '下移'}，总览与账号池将保持一致。` })
  }, [notify, visibleProviderIds])

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="主导航">
        <button className="brand" onClick={() => changePage('overview')} aria-label="返回监控总览">
          <span className="brand-mark"><CircleGauge size={23} /></span>
          <span><strong>SuperMonitor</strong><small>AI quota console</small></span>
        </button>
        <nav>
          {navItems.map(({ id, label, icon: Icon }) => (
            <button key={id} className={page === id ? 'nav-item active' : 'nav-item'} onClick={() => changePage(id)} aria-current={page === id ? 'page' : undefined} aria-label={label}>
              <Icon size={20} /><span>{label}</span>
              {id === 'alerts' && data?.alerts.length ? <b>{data.alerts.length}</b> : null}
            </button>
          ))}
        </nav>
        <div className="sidebar-foot"><ShieldCheck size={18} /><span>凭据本机加密</span><small>v0.10.0</small></div>
      </aside>

      <main className="main-stage">
        <header className="topbar">
          <div className="connection-state" role="status" aria-label={`本地部署，${streamStatus === 'live' ? '实时连接' : streamStatus === 'offline' ? '连接已中断' : '正在连接'}`}><i className={`connection-dot ${streamStatus}`} aria-hidden="true" /><span>本地部署 · {streamStatus === 'live' ? '实时连接' : streamStatus === 'offline' ? '连接已中断' : '正在连接'}</span></div>
          <div className="topbar-actions">
            {authentication.required ? <button className="icon-button" onClick={() => void logout()} title="退出管理会话" aria-label="退出管理会话"><LogOut size={19} /><span>退出</span></button> : null}
            <button className="icon-button" onClick={cycleTheme} title={`当前主题：${theme}`} aria-label="切换主题">
              {theme === 'light' ? <Sun size={20} /> : theme === 'dark' ? <Moon size={20} /> : <Settings size={20} />}
              <span>{theme === 'light' ? '浅色' : theme === 'dark' ? '深色' : '跟随系统'}</span>
            </button>
            <button className="refresh-button" onClick={() => void refreshAll()} disabled={refreshing}>
              <RefreshCw size={19} className={refreshing ? 'spin' : ''} />{refreshing ? '刷新中' : '刷新全部'}
            </button>
          </div>
        </header>

        <section className="page-heading">
          <div><h1>{pageTitles[page].title}</h1><p>{pageTitles[page].description}</p></div>
          <div className="data-clock"><Clock3 size={18} /><span>最近同步</span><strong>{data ? new Date(data.generatedAt).toLocaleTimeString('zh-CN', { hour12: false }) : '--:--:--'}</strong></div>
        </section>
        {error ? <div className="error-banner"><AlertTriangle size={20} /><span>{error}</span><button onClick={() => void retry()}>重试</button></div> : null}

        <div className="page-surface" key={page}>
          {loading || !data ? <DashboardSkeleton /> : (
            <PageContent page={page} data={data} providerOrder={providerOrder} onMoveProvider={moveProvider} onSelectAccount={setSelectedAccount} onReload={() => void retry(true)} />
          )}
        </div>
      </main>
      {selectedAccount ? <AccountDrawer account={selectedAccount} onClose={() => setSelectedAccount(null)} onReload={() => void retry(true)} /> : null}
    </div>
  )
}

function PageContent({ page, data, providerOrder, onMoveProvider, onSelectAccount, onReload }: { page: Page; data: Overview; providerOrder: string[]; onMoveProvider: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void; onSelectAccount: (account: AccountSummary) => void; onReload: () => void }) {
  if (page === 'overview') return <OverviewPage data={data} providerOrder={providerOrder} onMoveProvider={onMoveProvider} onSelectAccount={onSelectAccount} />
  if (page === 'accounts') return <AccountsPage accounts={data.accounts} providerOrder={providerOrder} onMoveProvider={onMoveProvider} onSelectAccount={onSelectAccount} onReload={onReload} />
  if (page === 'usage') return <UsagePage data={data} />
  if (page === 'activities') return <ActivitiesPage onReload={onReload} />
  if (page === 'alerts') return <AlertsPage alerts={data.alerts} />
  return <SettingsPage />
}

function OverviewPage({ data, providerOrder, onMoveProvider, onSelectAccount }: { data: Overview; providerOrder: string[]; onMoveProvider: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void; onSelectAccount: (account: AccountSummary) => void }) {
  return <>
    <KPIBand data={data} />
    <div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div>
    <section className="section-block">
      <div className="section-header"><div><h2>额度窗口</h2><p>颜色按剩余比例变化，时间为平台返回的精确重置或到期时间。</p></div></div>
      {data.accounts.length ? <AccountQuotaGroups accounts={data.accounts} providerOrder={providerOrder} onMoveProvider={onMoveProvider} onSelectAccount={onSelectAccount} /> : <EmptyState title="暂无真实额度" detail="连接账号后，这里会按账号显示平台返回的余额、积分或限额窗口。" />}
    </section>
  </>
}

function KPIBand({ data }: { data: Overview }) {
  return <section className="kpi-band">{data.kpis.map((kpi) => <div className={`kpi-item tone-${kpi.tone}`} key={kpi.id}><span>{kpi.label}</span><strong>{formatMetric(kpi.value, kpi.unit)}</strong><small>{kpi.unit}</small></div>)}<div className="kpi-context"><Database size={20} /><div><strong>计量口径隔离</strong><span>Token、余额、积分和订阅限额分别统计</span></div></div></section>
}

function TokenChart({ data }: { data: Overview }) {
  const [range, setRange] = useState<UsageRange>('30d')
  const visibleData = useMemo(() => filterUsageByRange(data.tokenTrend, range), [data.tokenTrend, range])
  const showYear = range === 'all'
  const rangeControl = <div className="usage-range-switch" role="group" aria-label="Token 轨迹时间范围">
    {usageRangeOptions.map((option) => <button type="button" key={option.value} aria-pressed={range === option.value} onClick={() => setRange(option.value)}>{option.label}</button>)}
  </div>

  return <section className="instrument-panel token-panel">
    <div className="panel-header token-chart-header"><div><h2>Token 轨迹</h2><p>仅统计平台返回或本地日志解析的真实计数 · {usageRangeDescription(range, visibleData.length)}</p></div>{rangeControl}</div>
    {visibleData.length ? <div className="chart-wrap"><ResponsiveContainer width="100%" height="100%"><ComposedChart data={visibleData} margin={{ top: 10, right: 6, left: -8, bottom: 0 }}><CartesianGrid stroke="var(--chart-grid)" vertical={false} /><XAxis dataKey="date" tickFormatter={(value: string) => showYear ? value.slice(0, 7).replace('-', '/') : value.slice(5)} tick={{ fill: 'var(--text-muted)', fontSize: 13 }} axisLine={false} tickLine={false} minTickGap={range === '7d' ? 12 : range === '30d' ? 26 : 48} /><YAxis yAxisId="tokens" tickFormatter={compactNumber} tick={{ fill: 'var(--text-muted)', fontSize: 13 }} axisLine={false} tickLine={false} /><YAxis yAxisId="requests" hide orientation="right" /><Tooltip content={<TokenTooltip />} /><Bar yAxisId="tokens" dataKey="cacheTokens" stackId="tokens" fill="var(--chart-cache)" /><Bar yAxisId="tokens" dataKey="inputTokens" stackId="tokens" fill="var(--chart-input)" /><Bar yAxisId="tokens" dataKey="outputTokens" stackId="tokens" fill="var(--chart-output)" radius={[4, 4, 0, 0]} /><Line yAxisId="requests" dataKey="requests" stroke="var(--chart-line)" strokeWidth={2} dot={false} /></ComposedChart></ResponsiveContainer></div> : <EmptyState title={data.tokenTrend.length ? '此时间段暂无 Token 用量' : '暂无 Token 用量'} detail={data.tokenTrend.length ? '切换到更长的时间范围，或等待新的用量记录写入。' : '平台返回可验证的 Token 用量后会生成趋势图。'} />}
  </section>
}

function TokenTooltip({ active, payload, label }: { active?: boolean; payload?: { name: string; value: number; color: string }[]; label?: string }) {
  if (!active || !payload) return null
  return <div className="chart-tooltip"><strong>{label}</strong>{payload.map((item) => <span key={item.name}><i style={{ background: item.color }} />{item.name}<b>{compactNumber(item.value)}</b></span>)}</div>
}

function ModelDonut({ data }: { data: Overview }) {
  const total = data.modelUsage.reduce((sum, item) => sum + item.tokens, 0)
  if (!data.modelUsage.length || total <= 0) return <section className="instrument-panel model-panel"><div className="panel-header"><div><h2>模型用量分布</h2><p>仅统计有真实模型归属的 Token</p></div></div><EmptyState title="暂无模型分布" detail="平台返回模型字段，或导入 Codex 本地会话用量后，会自动生成占比。" /></section>
  return <section className="instrument-panel model-panel"><div className="panel-header"><div><h2>模型用量分布</h2><p>来自平台字段或本地会话日志，不按额度百分比推算</p></div></div><div className="donut-wrap"><ResponsiveContainer width="100%" height={220}><PieChart><Pie data={data.modelUsage} dataKey="tokens" nameKey="model" innerRadius={65} outerRadius={92} paddingAngle={2} stroke="none">{data.modelUsage.map((entry) => <Cell key={entry.model} fill={entry.color} />)}</Pie><Tooltip formatter={(value) => compactNumber(Number(value))} /></PieChart></ResponsiveContainer><div className="donut-total"><span>自接入起</span><strong>{compactNumber(total)}</strong><small>tokens</small></div></div><div className="model-list">{data.modelUsage.map((item) => <div key={item.model}><i style={{ background: item.color }} /><span>{item.model}</span><b>{((item.tokens / total) * 100).toFixed(1)}%</b></div>)}</div></section>
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

function AccountQuotaGroups({ accounts, providerOrder, onMoveProvider, onSelectAccount }: { accounts: AccountSummary[]; providerOrder: string[]; onMoveProvider: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void; onSelectAccount: (account: AccountSummary) => void }) {
  const groups = useMemo(() => {
    const result = new Map<string, AccountSummary[]>()
    accounts.forEach((account) => result.set(account.providerId, [...(result.get(account.providerId) ?? []), account]))
    return sortProviderEntries([...result.entries()], providerOrder)
  }, [accounts, providerOrder])
  return <div className="quota-groups">{groups.map(([providerId, items], index) => <section className="quota-provider-group" key={providerId}><div className="quota-provider-heading"><div className="provider-section-title"><ProviderLogo providerId={providerId} name={items[0].provider} /><span><strong>{items[0].provider}</strong><small>{items.length} 个账号</small></span></div><ProviderOrderControls providerId={providerId} providerName={items[0].provider} index={index} total={groups.length} onMove={onMoveProvider} /></div><div className="quota-account-grid">{items.map((account) => <AccountQuotaCard key={account.id} account={account} onSelect={() => onSelectAccount(account)} />)}</div></section>)}</div>
}

function AccountQuotaCard({ account, onSelect }: { account: AccountSummary; onSelect: () => void }) {
  const isWorkBuddy = account.providerId === 'workbuddy-cn' || account.providerId === 'workbuddy-global'
  const signals = isWorkBuddy ? account.quotaWindows.slice(0, 1) : account.quotaWindows
  const hiddenPackages = Math.max(0, account.quotaWindows.length - signals.length)
  return <button className={`quota-account-card status-${account.status}`} onClick={onSelect}>
    <div className="quota-account-identity"><span className="provider-chip">{providerShortName(account.providerId, account.provider)}</span><strong>{account.email || account.alias}</strong><ChevronRight size={17} /></div>
    <div className="quota-account-plan"><span>套餐</span><b>{planLabel(account.plan)}</b></div>
    <div className="quota-account-signals">{signals.length ? signals.map((signal) => <AccountQuotaLine key={signal.id} signal={signal} />) : <div className="metric-summary"><strong>{account.primaryMetric}</strong><span>{account.secondaryMetric}</span></div>}</div>
    {hiddenPackages ? <span className="package-summary">总积分已包含 {hiddenPackages} 个官方积分包，点击查看明细</span> : null}
    <div className="quota-account-foot"><span>更新于 {relativeTime(account.lastRefreshedAt)}</span><span>{authMethodLabel(account.authMethod)}</span></div>
  </button>
}

function AccountQuotaLine({ signal }: { signal: QuotaSignal }) {
  const percent = signal.remainingPercent ?? (signal.total ? signal.value / signal.total * 100 : undefined)
  const tone = percent === undefined ? signal.status : percent <= 15 ? 'critical' : percent <= 35 ? 'warning' : 'healthy'
  const deadline = signal.resetAt ?? signal.expiresAt
  const displayValue = signal.kind === 'rate_window' && percent !== undefined ? `${Math.round(percent)}%` : quotaValue(signal.value, signal.unit)
  return <div className={`account-quota-line tone-${tone}`}><div><strong>{signal.label}</strong><span>{displayValue}</span>{deadline ? <time>{formatCompactDate(deadline)}</time> : null}</div>{percent !== undefined ? <div className="progress-track" role="progressbar" aria-valuenow={percent} aria-valuemin={0} aria-valuemax={100}><span style={{ transform: `scaleX(${Math.max(1, Math.min(100, percent)) / 100})` }} /></div> : null}</div>
}

function AccountGrid({ accounts, onSelectAccount }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void }) {
  if (!accounts.length) return <EmptyState title="还没有账号" detail="从下方平台列表添加你的第一个账号。" />
  return <div className="account-grid">{accounts.map((account) => <AccountQuotaCard key={account.id} account={account} onSelect={() => onSelectAccount(account)} />)}</div>
}

function ProviderAccountSections({ accounts, providerOrder, onMoveProvider, onSelectAccount }: { accounts: AccountSummary[]; providerOrder: string[]; onMoveProvider: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void; onSelectAccount: (account: AccountSummary) => void }) {
  if (!accounts.length) return <EmptyState title="还没有账号" detail="从下方平台列表添加你的第一个账号。" />
  const groups = new Map<string, AccountSummary[]>()
  accounts.forEach((account) => groups.set(account.providerId, [...(groups.get(account.providerId) ?? []), account]))
  const orderedGroups = sortProviderEntries([...groups.entries()], providerOrder)
  return <div className="provider-account-sections">{orderedGroups.map(([providerId, items], index) => <section className="provider-account-section" key={providerId}><header><div className="provider-section-title"><ProviderLogo providerId={providerId} name={items[0].provider} /><span><strong>{items[0].provider}</strong><small>{items.length} 个已连接账号</small></span></div><div className="provider-heading-actions"><span className="adapter-state live">实时额度</span><ProviderOrderControls providerId={providerId} providerName={items[0].provider} index={index} total={orderedGroups.length} onMove={onMoveProvider} /></div></header><AccountGrid accounts={items} onSelectAccount={onSelectAccount} /></section>)}</div>
}

function ProviderOrderControls({ providerId, providerName, index, total, onMove }: { providerId: string; providerName: string; index: number; total: number; onMove: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void }) {
  return <div className="provider-order-controls" role="group" aria-label={`${providerName} 显示顺序`}>
    <span>顺序</span>
    <button type="button" onClick={() => onMove(providerId, 'up', providerName)} disabled={index === 0} aria-label={`将 ${providerName} 上移`} title="向前显示"><ArrowUp size={15} /></button>
    <button type="button" onClick={() => onMove(providerId, 'down', providerName)} disabled={index === total - 1} aria-label={`将 ${providerName} 下移`} title="向后显示"><ArrowDown size={15} /></button>
  </div>
}

function AccountsPage({ accounts, providerOrder, onMoveProvider, onSelectAccount, onReload }: { accounts: AccountSummary[]; providerOrder: string[]; onMoveProvider: (providerId: string, direction: ProviderOrderDirection, providerName: string) => void; onSelectAccount: (account: AccountSummary) => void; onReload: () => void }) {
  const [providers, setProviders] = useState<Provider[]>([])
  const [providersLoading, setProvidersLoading] = useState(true)
  const [providersError, setProvidersError] = useState('')
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Provider | null>(null)
  const loadProviders = useCallback(async () => {
    setProvidersLoading(true)
    setProvidersError('')
    try {
      const value = await api.providers()
      setProviders(value.items)
    } catch (reason) {
      setProvidersError(reason instanceof Error ? reason.message : '平台列表读取失败')
    } finally {
      setProvidersLoading(false)
    }
  }, [])
  useEffect(() => { void loadProviders() }, [loadProviders])
  const filtered = useMemo(() => providers.filter((provider) => `${provider.name}${provider.category}${provider.description}`.toLowerCase().includes(query.toLowerCase())), [providers, query])
  const groups = ['国内平台', '国际平台']
  const visibleGroups = groups.map((group) => ({ group, providers: filtered.filter((provider) => provider.category === group) })).filter((entry) => entry.providers.length)
  return <>
    <section className="section-block"><div className="section-header"><div><h2>已连接账号</h2><p>按应用归类 · {accounts.length} 个真实账号 · 顺序会自动保存</p></div></div><ProviderAccountSections accounts={accounts} providerOrder={providerOrder} onMoveProvider={onMoveProvider} onSelectAccount={onSelectAccount} /></section>
    <section className="section-block provider-section"><div className="section-header"><div><h2>添加平台账号</h2><p>每个平台独立添加；只有专属认证与真实额度字段完成验证后才开放连接。</p></div><label className="provider-search"><Search size={17} /><input value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索平台" aria-label="搜索可添加平台" /></label></div>
      {providersLoading ? <div className="provider-load-state" role="status"><LoaderCircle className="spin" size={21} /><span><strong>正在读取平台列表</strong><small>加载可用认证方式与额度能力</small></span></div> : providersError ? <div className="provider-load-state error" role="alert"><AlertTriangle size={21} /><span><strong>平台列表暂时无法读取</strong><small>{providersError}</small></span><button className="secondary-button" type="button" onClick={() => void loadProviders()}><RefreshCw size={16} />重新加载</button></div> : visibleGroups.length ? visibleGroups.map(({ group, providers: groupProviders }) => <div key={group} className="provider-group"><h3>{group}</h3><div className="provider-grid">{groupProviders.map((provider) => <article className={`provider-card ${provider.liveAuth ? '' : 'is-researching'}`} key={provider.id}><div className="provider-card-top"><ProviderLogo providerId={provider.id} name={provider.name} /><span className={`adapter-state ${provider.liveAuth ? 'live' : 'pending'}`}>{provider.liveAuth ? '可连接' : '接入验证中'}</span></div><h4>{provider.name}</h4><p>{provider.description}</p><div className="capability-row">{provider.capabilities.slice(0, 3).map((capability) => <span key={capability}>{capabilityLabel(capability)}</span>)}</div><button className={provider.liveAuth ? 'provider-action active' : 'provider-action'} onClick={() => provider.liveAuth && setSelected(provider)} disabled={!provider.liveAuth}>{provider.liveAuth ? <><Plus size={17} />添加账号</> : '专属接入尚未开放'}</button></article>)}</div></div>) : <EmptyState title="没有匹配的平台" detail="请尝试平台全称、英文名或所属地区。" />}
    </section>
    {selected ? <ProviderConnectDialog provider={selected} onClose={() => setSelected(null)} onConnected={() => { setSelected(null); onReload() }} /> : null}
  </>
}

function ProviderConnectDialog({ provider, onClose, onConnected }: { provider: Provider; onClose: () => void; onConnected: () => void }) {
  const { notify } = useToast()
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

  useDeviceLoginPolling({ session, providerName: provider.name, setSession, setBusy, setError, notify, onConnected })

  const importFile = async () => {
    if (!file) { setError('请先选择认证文件'); return }
    setBusy(true); setError('')
    try {
      const account = await api.importCredential(provider.id, file, alias)
      setFile(null)
      notify({ tone: 'success', title: '认证文件导入成功', message: `${account.alias || provider.name} 已加入账号池并完成首次额度读取。` })
      onConnected()
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '导入失败'
      setError(message)
      setBusy(false)
      notify({ tone: 'error', title: '认证文件导入失败', message })
    }
  }
  const startOAuth = async () => {
    setBusy(true); setError('')
    try {
      const next = isCodex ? await api.startCodexDeviceLogin(alias) : await api.startProviderOAuth(provider.id, alias)
      setSession(next)
      setBusy(false)
      notify({ tone: 'info', title: `${provider.name} 授权已启动`, message: '请在官方页面完成登录，本页面会自动读取授权结果。' })
      if (!isWorkBuddy) window.open(next.verifyUrl, '_blank', 'noopener,noreferrer')
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '无法启动登录'
      setError(message)
      setBusy(false)
      notify({ tone: 'error', title: `${provider.name} 授权无法启动`, message })
    }
  }
  const connectSecret = async () => {
    if (!secret.trim() || (provider.id === 'bailian' && !secret2.trim())) { setError(secretConfig(provider.id).emptyError); return }
    setBusy(true); setError('')
    try {
      const account = await api.connectSecret(provider.id, alias, secret, secret2)
      setSecret('')
      setSecret2('')
      notify({ tone: 'success', title: `${provider.name} 连接成功`, message: `${account.alias || provider.name} 已加入账号池并完成真实额度读取。` })
      onConnected()
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '连接失败'
      setError(message)
      setBusy(false)
      notify({ tone: 'error', title: `${provider.name} 连接失败`, message })
    }
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
    zhipu: { title: '智谱开放平台 API Key', detail: '优先读取 Coding Plan 额度；普通开放平台 Key 会读取官方人民币可用余额，余额接口不可用时再验证模型访问。', placeholder: '粘贴 open.bigmodel.cn API Key', action: '验证 API Key 并读取额度', emptyError: '请输入智谱 API Key' },
    mimo: { title: '小米 MiMo 控制台 Cookie', detail: '仅用于读取小米 MiMo Token Plan；与华为基元律动完全独立。平台未提供可用于额度读取的 OAuth，因此提供官方登录跳转与逐步获取教程。', placeholder: '在 platform.xiaomimimo.com 登录后复制请求 Cookie', action: '验证并读取 Token Plan', emptyError: '请粘贴小米 MiMo 控制台 Cookie', multiline: true, loginUrl: 'https://platform.xiaomimimo.com/' },
    tokenrhythm: { title: '基元律动网页登录态', detail: '华为基元律动 tokenrhythm.studio；支持 sess_ 会话令牌或 tr_session / tr_ref_device Cookie。与小米 MiMo 完全独立。', placeholder: 'sess_...\n或 tr_session=sess_...; tr_ref_device=...', action: '验证并读取人民币余额', emptyError: '请粘贴基元律动 sess_ 会话令牌或 Cookie', multiline: true, loginUrl: 'https://tokenrhythm.studio/' },
  }
  return copies[providerId] ?? { title: '认证信息', detail: '用于读取真实额度。', placeholder: '', action: '验证并连接', emptyError: '请输入认证信息' }
}

function UsagePage({ data }: { data: Overview }) {
  const hasCodex = data.accounts.some((account) => account.providerId === 'codex')
  return <>
    <KPIBand data={data} />
    {hasCodex ? <section className="usage-truth-note"><ShieldCheck size={20} /><div><strong>Codex 额度与 Token 用量来自不同数据源</strong><p>OpenAI 的额度接口只返回限额百分比，不返回模型与 Token。要让真实的 gpt-5.6-sol 等模型进入下方统计，请在对应 Codex 账号详情中导入本机 <code>.codex/sessions</code> 文件夹；对话正文只在浏览器本地读取，不会上传。</p></div></section> : null}
    <div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div>
  </>
}
function ActivitiesPage({ onReload }: { onReload: () => void }) {
  const { notify } = useToast()
  const [items, setItems] = useState<ActivityItem[]>([])
  const [loading, setLoading] = useState(true)
  const [running, setRunning] = useState<string[]>([])
  const [error, setError] = useState('')
  const load = () => { setLoading(true); setError(''); void api.activities().then((payload) => { setItems(payload.items); setError(payload.warning ?? '') }).catch((reason: Error) => setError(reason.message)).finally(() => setLoading(false)) }
  useEffect(load, [])
  const run = async (item: ActivityItem, announce = true) => {
    setRunning((current) => [...current, item.accountId]); setError('')
    try {
      const next = await api.runActivity(item.accountId, item.id)
      setItems((current) => current.map((entry) => entry.accountId === item.accountId && entry.id === item.id ? next : entry))
      onReload()
      if (announce) notify({ tone: 'success', title: `${item.title}执行成功`, message: `${item.accountAlias} 的活动状态和额度已更新。` })
      return true
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '活动执行失败'
      setError(message)
      if (announce) notify({ tone: 'error', title: `${item.title}执行失败`, message })
      return false
    }
    finally { setRunning((current) => current.filter((id) => id !== item.accountId)) }
  }
  const available = items.filter((item) => item.status === 'available')
  const runAll = async () => {
    let succeeded = 0
    for (const item of available) if (await run(item, false)) succeeded += 1
    const failed = available.length - succeeded
    notify({
      tone: failed ? 'error' : 'success',
      title: failed ? '部分活动未完成' : '全部活动执行成功',
      message: `成功 ${succeeded} 项${failed ? `，失败 ${failed} 项，请查看页面错误后重试。` : '，额度状态已同步。'}`,
    })
  }
  return <section className="section-block"><div className="section-header"><div><h2>可执行活动</h2><p>只展示后端已通过官方接口确认存在的真实活动。</p></div><button className="primary-button" onClick={() => void runAll()} disabled={!available.length || running.length > 0}>{running.length ? <LoaderCircle className="spin" size={17} /> : <Zap size={17} />}全部签到</button></div>
    {error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}
    {loading ? <div className="activity-loading"><LoaderCircle className="spin" size={20} />正在读取平台活动状态</div> : items.length ? <div className="activity-grid">{items.map((item) => { const busy = running.includes(item.accountId); const completed = item.status === 'completed'; const unavailable = item.status === 'error'; return <article className={`activity-card ${completed ? 'completed' : unavailable ? 'error' : ''}`} key={`${item.accountId}-${item.id}`}><div className="activity-icon">{completed ? <Check size={20} /> : unavailable ? <AlertTriangle size={20} /> : <Zap size={20} />}</div><div className="activity-copy"><span>{item.provider}</span><strong>{item.accountAlias} · {item.title}</strong><small>{item.description}</small></div><button className={completed ? 'secondary-button completed' : unavailable ? 'secondary-button' : 'primary-button'} onClick={() => void run(item)} disabled={completed || unavailable || busy}>{busy ? <LoaderCircle className="spin" size={17} /> : completed ? <Check size={17} /> : unavailable ? <AlertTriangle size={17} /> : <Zap size={17} />}{completed ? '今日已完成' : unavailable ? '状态读取失败' : '立即签到'}</button></article> })}</div> : <EmptyState title="当前账号池没有可执行活动" detail="Codex 等没有签到活动的平台不会出现在这里；接入支持活动的国内 WorkBuddy 账号后会自动显示。" />}
  </section>
}
function AlertsPage({ alerts }: { alerts: Alert[] }) { return <section className="section-block alert-list"><div className="section-header"><div><h2>未解决告警</h2><p>同一故障自动去重；额度恢复或提醒窗口结束后会自动移除。</p></div></div>{alerts.length ? alerts.map((alert) => <article key={alert.id} className={`alert-item severity-${alert.severity}`}><AlertTriangle size={22} /><div><span>{alert.provider} · {relativeTime(alert.createdAt)}</span><strong>{alert.title}</strong><p>{alert.message}</p><small>建议：{alert.recovery}</small></div></article>) : <EmptyState title="当前没有未解决告警" detail="额度、认证和刷新状态正常；新告警会自动出现在这里。" />}</section> }
function SettingsPage() {
  const { notify } = useToast()
  const [channels, setChannels] = useState<NotificationChannel[]>([])
  const [policy, setPolicy] = useState<NotificationPolicy | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [creating, setCreating] = useState<NotificationChannelKind | null>(null)
  const [testingID, setTestingID] = useState('')
  const [deletingID, setDeletingID] = useState('')
  const [confirmDeleteID, setConfirmDeleteID] = useState('')
  const [scanning, setScanning] = useState(false)
  const [scanError, setScanError] = useState('')
  const [scanResult, setScanResult] = useState<NotificationEvaluation | null>(null)
  const [formErrors, setFormErrors] = useState<Partial<Record<NotificationChannelKind, string>>>({})
  const [feishu, setFeishu] = useState({ name: '', webhookUrl: '' })
  const [mail, setMail] = useState({ name: '', sender: '', authCode: '', recipient: '' })

  const loadSettings = useCallback(async () => {
    setLoading(true)
    setLoadError('')
    try {
      const [channelPayload, nextPolicy] = await Promise.all([api.notificationChannels(), api.notificationPolicy()])
      setChannels(channelPayload.items)
      setPolicy(nextPolicy)
    } catch (reason) {
      setLoadError(reason instanceof Error ? reason.message : '通知设置读取失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { void loadSettings() }, [loadSettings])

  const createChannel = async (kind: NotificationChannelKind, event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (creating) return
    const error = kind === 'feishu'
      ? (!feishu.webhookUrl.trim() ? '请输入飞书机器人 Webhook 地址' : '')
      : (!mail.sender.trim() ? '请输入 QQ 发件邮箱' : !mail.sender.trim().toLowerCase().endsWith('@qq.com') ? '发件人必须是 QQ 邮箱地址' : !mail.authCode.trim() ? '请输入 SMTP 授权码' : !mail.recipient.trim() ? '请输入收件邮箱' : '')
    if (error) {
      setFormErrors((current) => ({ ...current, [kind]: error }))
      notify({ tone: 'error', title: '通知渠道信息不完整', message: error })
      return
    }
    setFormErrors((current) => ({ ...current, [kind]: '' }))
    setCreating(kind)
    try {
      const channel = kind === 'feishu'
        ? await api.createNotificationChannel({ kind, name: feishu.name.trim(), webhookUrl: feishu.webhookUrl.trim() })
        : await api.createNotificationChannel({ kind, name: mail.name.trim(), sender: mail.sender.trim(), authCode: mail.authCode.trim(), recipient: mail.recipient.trim() })
      setChannels((current) => [channel, ...current])
      if (kind === 'feishu') setFeishu({ name: '', webhookUrl: '' })
      else setMail({ name: '', sender: '', authCode: '', recipient: '' })
      notify({ tone: 'success', title: '通知渠道连接成功', message: `“${channel.name}”已验证并加密保存。` })
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '通知渠道连接失败'
      setFormErrors((current) => ({ ...current, [kind]: message }))
      notify({ tone: 'error', title: '通知渠道连接失败', message })
    } finally {
      setCreating(null)
    }
  }

  const testChannel = async (channel: NotificationChannel) => {
    if (testingID || deletingID) return
    setTestingID(channel.id)
    try {
      const result = await api.testNotificationChannel(channel.id)
      notify({ tone: 'success', title: '测试通知发送成功', message: `${channel.name}：${result.message}` })
    } catch (reason) {
      notify({ tone: 'error', title: '测试通知发送失败', message: reason instanceof Error ? reason.message : '请检查渠道配置后重试。' })
    } finally {
      setTestingID('')
    }
  }

  const deleteChannel = async (channel: NotificationChannel) => {
    if (testingID || deletingID) return
    setDeletingID(channel.id)
    try {
      await api.deleteNotificationChannel(channel.id)
      setChannels((current) => current.filter((item) => item.id !== channel.id))
      setConfirmDeleteID('')
      notify({ tone: 'success', title: '通知渠道已删除', message: `“${channel.name}”已从本机移除。` })
    } catch (reason) {
      notify({ tone: 'error', title: '通知渠道删除失败', message: reason instanceof Error ? reason.message : '请稍后重试。' })
    } finally {
      setDeletingID('')
    }
  }

  const evaluateNow = async () => {
    if (scanning) return
    setScanError('')
    setScanResult(null)
    setScanning(true)
    try {
      const result = await api.evaluateNotifications()
      setScanResult(result)
      notify({ tone: 'success', title: '告警与活动扫描完成', message: `检查 ${result.checkedAccounts} 个账号，发现 ${result.availableActivities} 项可用活动。` })
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '扫描失败，请稍后重试'
      setScanError(message)
      notify({ tone: 'error', title: '告警与活动扫描失败', message })
    } finally {
      setScanning(false)
    }
  }

  if (loading) return <div className="settings-loading" role="status" aria-live="polite"><LoaderCircle className="spin" size={22} /><span><strong>正在读取通知设置</strong><small>加载本机保存的渠道和固定告警策略</small></span></div>
  if (loadError || !policy) return <div className="settings-load-error" role="alert"><AlertTriangle size={22} /><div><strong>通知设置暂时无法读取</strong><p>{loadError || '未返回告警策略'}</p><button className="secondary-button" onClick={() => void loadSettings()}><RefreshCw size={16} />重新加载</button></div></div>

  const channelBusy = Boolean(testingID || deletingID)
  return <div className="notification-settings">
    <section className="notification-command" aria-labelledby="notification-command-title">
      <div><span className="notification-command-icon"><Bell size={21} /></span><span><h2 id="notification-command-title">通知与自动活动</h2><p>额度预警、重置提醒和已验证活动由本机定时扫描，命中策略后发送到已连接渠道。</p></span></div>
      <button className="primary-button" onClick={() => void evaluateNow()} disabled={scanning} aria-describedby="notification-scan-note">{scanning ? <LoaderCircle className="spin" size={17} /> : <ScanSearch size={17} />}{scanning ? '正在扫描' : '立即扫描'}</button>
    </section>

    <div className="notification-layout">
      <div className="notification-main">
        <section className="settings-panel channel-panel" aria-labelledby="channel-list-title">
          <header><div><h2 id="channel-list-title">通知渠道</h2><p>渠道凭据不会回显；这里只展示脱敏后的投递目标。</p></div><span className="channel-count">{channels.length} 个</span></header>
          {channels.length ? <div className="notification-channel-list">{channels.map((channel) => {
            const testing = testingID === channel.id
            const deleting = deletingID === channel.id
            const confirming = confirmDeleteID === channel.id
            return <article className="notification-channel" key={channel.id}>
              <span className={`channel-kind-icon ${channel.kind}`}>{channel.kind === 'feishu' ? <Bot size={20} /> : <Mail size={20} />}</span>
              <div className="channel-copy"><span>{channel.kind === 'feishu' ? '飞书机器人' : 'QQ 邮箱 SMTP'} · {channel.enabled ? '已启用' : '已停用'}</span><strong>{channel.name}</strong><small>{channel.target}</small><time>更新于 {formatDateTime(channel.updatedAt)}</time></div>
              {confirming ? <div className="channel-delete-confirm" role="group" aria-label={`确认删除 ${channel.name}`}><strong>删除“{channel.name}”？</strong><span><button className="text-button" onClick={() => setConfirmDeleteID('')} disabled={deleting} autoFocus>取消</button><button className="danger-button" onClick={() => void deleteChannel(channel)} disabled={deleting}>{deleting ? <LoaderCircle className="spin" size={15} /> : <Trash2 size={15} />}{deleting ? '删除中' : '确认删除'}</button></span></div> : <div className="channel-actions"><button className="secondary-button" onClick={() => void testChannel(channel)} disabled={channelBusy} aria-label={`测试 ${channel.name}`}>{testing ? <LoaderCircle className="spin" size={16} /> : <Send size={16} />}{testing ? '发送中' : '发送测试'}</button><button className="channel-delete-button" onClick={() => setConfirmDeleteID(channel.id)} disabled={channelBusy} aria-label={`删除 ${channel.name}`}><Trash2 size={17} /></button></div>}
            </article>
          })}</div> : <div className="notification-empty"><Bell size={27} /><strong>还没有通知渠道</strong><span>在下方连接飞书机器人或 QQ 邮箱后，告警才会向外发送。</span></div>}
        </section>

        <section className="settings-panel channel-connect-panel" aria-labelledby="channel-connect-title">
          <header><div><h2 id="channel-connect-title">连接新渠道</h2><p>提交后立即写入本机加密保险箱，可在上方发送测试消息。</p></div></header>
          <div className="channel-form-grid">
            <form className="notification-form" onSubmit={(event) => void createChannel('feishu', event)} aria-labelledby="feishu-form-title" aria-busy={creating === 'feishu'}>
              <div className="notification-form-title"><span className="channel-kind-icon feishu"><Bot size={20} /></span><span><strong id="feishu-form-title">飞书自定义机器人</strong><small>使用官方群机器人 HTTPS Webhook</small></span></div>
              <label htmlFor="feishu-name">渠道名称 <small>可选</small></label>
              <input id="feishu-name" value={feishu.name} onChange={(event) => setFeishu((current) => ({ ...current, name: event.target.value }))} placeholder="例如：开发组告警" maxLength={80} disabled={creating !== null} />
              <label htmlFor="feishu-webhook">机器人 Webhook</label>
              <input id="feishu-webhook" type="password" autoComplete="off" value={feishu.webhookUrl} onChange={(event) => setFeishu((current) => ({ ...current, webhookUrl: event.target.value }))} placeholder="https://open.feishu.cn/open-apis/bot/v2/hook/..." disabled={creating !== null} required />
              {formErrors.feishu ? <div className="form-error" role="alert"><AlertTriangle size={16} />{formErrors.feishu}</div> : null}
              <button className="primary-button" type="submit" disabled={creating !== null || !feishu.webhookUrl.trim()}>{creating === 'feishu' ? <LoaderCircle className="spin" size={17} /> : <Bot size={17} />}{creating === 'feishu' ? '正在连接' : '连接飞书机器人'}</button>
            </form>

            <form className="notification-form" onSubmit={(event) => void createChannel('qq_mail', event)} aria-labelledby="qq-mail-form-title" aria-busy={creating === 'qq_mail'}>
              <div className="notification-form-title"><span className="channel-kind-icon qq_mail"><Mail size={20} /></span><span><strong id="qq-mail-form-title">QQ 邮箱 SMTP</strong><small>通过 smtp.qq.com:465 加密发送</small></span></div>
              <label htmlFor="mail-name">渠道名称 <small>可选</small></label>
              <input id="mail-name" value={mail.name} onChange={(event) => setMail((current) => ({ ...current, name: event.target.value }))} placeholder="例如：个人邮箱提醒" maxLength={80} disabled={creating !== null} />
              <div className="notification-field-pair"><span><label htmlFor="mail-sender">QQ 发件邮箱</label><input id="mail-sender" type="email" autoComplete="email" value={mail.sender} onChange={(event) => setMail((current) => ({ ...current, sender: event.target.value }))} placeholder="name@qq.com" disabled={creating !== null} required /></span><span><label htmlFor="mail-recipient">收件邮箱</label><input id="mail-recipient" type="email" autoComplete="email" value={mail.recipient} onChange={(event) => setMail((current) => ({ ...current, recipient: event.target.value }))} placeholder="接收提醒的邮箱" disabled={creating !== null} required /></span></div>
              <label htmlFor="mail-auth-code">SMTP 授权码 <small>不是 QQ 密码</small></label>
              <input id="mail-auth-code" type="password" autoComplete="new-password" value={mail.authCode} onChange={(event) => setMail((current) => ({ ...current, authCode: event.target.value }))} placeholder="在 QQ 邮箱设置中生成的授权码" maxLength={128} disabled={creating !== null} required />
              {formErrors.qq_mail ? <div className="form-error" role="alert"><AlertTriangle size={16} />{formErrors.qq_mail}</div> : null}
              <button className="primary-button" type="submit" disabled={creating !== null || !mail.sender.trim() || !mail.authCode.trim() || !mail.recipient.trim()}>{creating === 'qq_mail' ? <LoaderCircle className="spin" size={17} /> : <Mail size={17} />}{creating === 'qq_mail' ? '正在连接' : '连接 QQ 邮箱'}</button>
            </form>
          </div>
          <p className="notification-security-note"><ShieldCheck size={17} /><span><strong>敏感信息只保存在你的服务器</strong>Webhook 与 SMTP 授权码由本机 AES-GCM 加密，接口和页面只返回脱敏目标，不会回显密钥。</span></p>
        </section>
      </div>

      <aside className="notification-sidebar">
        <section className="settings-panel policy-panel" aria-labelledby="policy-title">
          <header><div><h2 id="policy-title">固定告警策略</h2><p>当前版本由服务端统一执行，避免不同浏览器产生冲突。</p></div><span className="policy-lock"><KeyRound size={14} />只读</span></header>
          <div className="policy-list">
            <div><CircleGauge size={18} /><span><strong>低额度预警</strong><small>任一可计算额度降至 {policy.lowQuotaPercent}% 或以下时提醒一次</small></span></div>
            <div><Clock3 size={18} /><span><strong>重置时间提醒</strong><small>距离重置 {policy.resetReminderDays.join(' 天、')} 天时各提醒一次</small></span></div>
            <div><RefreshCw size={18} /><span><strong>后台扫描频率</strong><small>服务持续运行时每 {policy.schedulerMinutes} 分钟检查一次</small></span></div>
            <div><Zap size={18} /><span><strong>自动活动{policy.autoActivities ? '已开启' : '已关闭'}</strong><small>当前只执行已验证的 WorkBuddy 国内版每日签到；其他平台接入真实活动适配器后才会出现。</small></span></div>
          </div>
          <p className="policy-scan-note" id="notification-scan-note">“立即扫描”会按以上策略检查全部已连接账号，并可能发送提醒或执行已验证签到。</p>
        </section>

        {scanError ? <section className="settings-panel scan-result-panel error" role="alert"><AlertTriangle size={21} /><div><strong>本次扫描未完成</strong><p>{scanError}</p><button className="secondary-button" onClick={() => void evaluateNow()} disabled={scanning}><RefreshCw size={16} />重新扫描</button></div></section> : null}
        {scanResult ? <section className={`settings-panel scan-result-panel ${scanResult.failedMessages ? 'warning' : 'success'}`} aria-live="polite"><Check size={21} /><div><strong>最近一次扫描结果</strong><p>检查 {scanResult.checkedAccounts} 个账号，触发 {scanResult.triggeredAlerts} 条提醒，成功投递 {scanResult.deliveredMessages} 条。</p><dl><div><dt>投递失败</dt><dd>{scanResult.failedMessages}</dd></div><div><dt>可用活动</dt><dd>{scanResult.availableActivities}</dd></div><div><dt>完成活动</dt><dd>{scanResult.completedActivities}</dd></div></dl></div></section> : null}
      </aside>
    </div>
  </div>
}

function AccountDrawer({ account, onClose, onReload }: { account: AccountSummary; onClose: () => void; onReload: () => void }) {
  const { notify } = useToast()
  const ref = useRef<HTMLElement>(null)
  const [refreshing, setRefreshing] = useState(false)
  const [error, setError] = useState('')
  const [deletePhase, setDeletePhase] = useState<'idle' | 'confirming' | 'deleting'>('idle')
  const [deleteError, setDeleteError] = useState('')
  const [importingUsage, setImportingUsage] = useState(false)
  const usageFilesRef = useRef<HTMLInputElement>(null)
  const usageFolderRef = useRef<HTMLInputElement>(null)
  const deleteDialogOpen = deletePhase !== 'idle'
  const deleting = deletePhase === 'deleting'
  useEffect(() => { ref.current?.focus() }, [])
  useEffect(() => {
    usageFolderRef.current?.setAttribute('webkitdirectory', '')
    usageFolderRef.current?.setAttribute('directory', '')
  }, [])
  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !deleteDialogOpen) onClose()
    }
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [deleteDialogOpen, onClose])
  const refreshAccount = async () => {
    setRefreshing(true)
    setError('')
    try {
      await api.refreshAccount(account.id)
      notify({ tone: 'success', title: '账号额度刷新成功', message: `${account.alias} 的最新额度和重置时间已同步。` })
      onReload()
      onClose()
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '刷新失败'
      setError(message)
      setRefreshing(false)
      notify({ tone: 'error', title: '账号额度刷新失败', message })
    }
  }
  const openDeleteDialog = () => { setError(''); setDeleteError(''); setDeletePhase('confirming') }
  const closeDeleteDialog = () => { if (!deleting) { setDeleteError(''); setDeletePhase('idle') } }
  const deleteAccount = async () => {
    if (deleting) return
    setDeletePhase('deleting')
    setDeleteError('')
    try {
      await api.deleteAccount(account.id)
      notify({ tone: 'success', title: '账号删除成功', message: `${account.email || account.alias} 的本机凭据、额度缓存和关联记录已清除。` })
      onClose()
      onReload()
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : '删除账号失败，请稍后重试'
      setDeleteError(message)
      setDeletePhase('confirming')
      notify({ tone: 'error', title: '账号删除失败', message })
    }
  }
  const importCodexUsage = async (files: FileList | null) => {
    if (!files?.length || importingUsage) return
    setImportingUsage(true)
    setError('')
    try {
      const parsed = await parseCodexUsageFiles([...files])
      if (!parsed.sources.length) throw new Error('所选内容中没有可识别且带真实模型字段的 Codex Token 记录')
      const result = { importedSources: 0, unchangedSources: 0, importedEntries: 0, importedTokens: 0 }
      for (let offset = 0; offset < parsed.sources.length; offset += 100) {
        const batch = await api.importCodexUsage(account.id, parsed.sources.slice(offset, offset + 100))
        result.importedSources += batch.importedSources
        result.unchangedSources += batch.unchangedSources
        result.importedEntries += batch.importedEntries
        result.importedTokens += batch.importedTokens
      }
      const unchanged = result.unchangedSources ? `，${result.unchangedSources} 个文件未变化` : ''
      const skipped = parsed.skippedFiles || parsed.skippedEvents ? `；跳过 ${parsed.skippedFiles} 个无用文件、${parsed.skippedEvents} 条缺少模型或日期的记录` : ''
      notify({ tone: 'success', title: 'Codex 真实用量导入成功', message: `更新 ${result.importedSources} 个会话文件，写入 ${compactNumber(result.importedTokens)} tokens${unchanged}${skipped}。` })
      onReload()
    } catch (reason) {
      const message = reason instanceof Error ? reason.message : 'Codex 用量导入失败'
      setError(message)
      notify({ tone: 'error', title: 'Codex 用量导入失败', message })
    } finally {
      setImportingUsage(false)
      if (usageFilesRef.current) usageFilesRef.current.value = ''
      if (usageFolderRef.current) usageFolderRef.current.value = ''
    }
  }
  const isWorkBuddy = account.providerId === 'workbuddy-cn' || account.providerId === 'workbuddy-global'
  const mainSignals = isWorkBuddy ? account.quotaWindows.slice(0, 1) : account.quotaWindows
  const packageSignals = isWorkBuddy ? account.quotaWindows.slice(1) : []
  return <>
    <div className="drawer-backdrop" onMouseDown={(event) => { if (!deleteDialogOpen && event.target === event.currentTarget) onClose() }}>
      <aside className="account-drawer" ref={ref} tabIndex={-1} aria-label={`${account.alias} 账号详情`}>
        <header><div><span>实时账号</span><h2>{account.alias}</h2><p>{account.email || account.provider} · {planLabel(account.plan || account.region)}</p></div><button className="close-button" onClick={onClose} aria-label="关闭账号详情" disabled={deleting}><X size={22} /></button></header>
        <div className="drawer-content">
          <div className="detail-row"><span>认证方式</span><strong>{authMethodLabel(account.authMethod)}</strong></div>
          <div className="detail-row"><span>数据来源</span><strong>{account.source}</strong></div>
          <div className="detail-row"><span>最近刷新</span><strong>{formatDateTime(account.lastRefreshedAt)}</strong></div>
          <div className="drawer-quotas">{mainSignals.length ? mainSignals.map((signal) => <QuotaProgress key={signal.id} signal={signal} />) : <EmptyState title={account.primaryMetric} detail={account.secondaryMetric} />}</div>
          {packageSignals.length ? <details className="package-details"><summary><span>官方积分包明细</span><b>{packageSignals.length} 个</b></summary><p>这些积分包来自官方 Billing 返回，金额已计入上方总积分，不会再次累加。</p><div>{packageSignals.map((signal) => <div className="package-row" key={signal.id}><span><strong>{signal.label}</strong><small>{signal.expiresAt ? `${formatCompactDate(signal.expiresAt)} 到期` : '未返回到期时间'}</small></span><b>{quotaValue(signal.value, signal.unit)}</b></div>)}</div></details> : null}
          {account.providerId === 'codex' ? <section className="codex-usage-import"><div><Upload size={19} /><span><strong>导入真实模型与 Token 用量</strong><small>额度接口不提供模型明细。选择本机 <code>.codex/sessions</code> 文件夹或 rollout JSONL；浏览器只上传日期、模型和计数聚合，不上传对话正文。</small></span></div><div className="codex-usage-actions"><button className="secondary-button" type="button" onClick={() => usageFolderRef.current?.click()} disabled={importingUsage}>{importingUsage ? <LoaderCircle className="spin" size={16} /> : <Boxes size={16} />}选择 sessions 文件夹</button><button className="secondary-button" type="button" onClick={() => usageFilesRef.current?.click()} disabled={importingUsage}><FileJson size={16} />选择 JSONL 文件</button></div><input ref={usageFolderRef} className="visually-hidden" type="file" multiple accept=".jsonl,application/x-ndjson" onChange={(event) => void importCodexUsage(event.target.files)} /><input ref={usageFilesRef} className="visually-hidden" type="file" multiple accept=".jsonl,application/x-ndjson" onChange={(event) => void importCodexUsage(event.target.files)} /></section> : null}
          {error ? <div className="form-error"><AlertTriangle size={17} />{error}</div> : null}
        </div>
        <footer className="drawer-footer">
          <button className="primary-button" onClick={() => void refreshAccount()} disabled={refreshing || deleting || importingUsage}>{refreshing ? <LoaderCircle className="spin" size={18} /> : <RefreshCw size={18} />}立即读取真实额度</button>
          {!account.synthetic ? <div className="danger-zone"><span><strong>删除本机账号</strong><small>清除认证凭据和额度记录，不会注销平台账号。</small></span><button className="danger-button" onClick={openDeleteDialog} disabled={refreshing || deleting}><Trash2 size={17} />删除账号</button></div> : null}
        </footer>
      </aside>
    </div>
    {deleteDialogOpen ? <DeleteAccountDialog account={account} busy={deleting} error={deleteError} onCancel={closeDeleteDialog} onConfirm={() => void deleteAccount()} /> : null}
  </>
}

function DeleteAccountDialog({ account, busy, error, onCancel, onConfirm }: { account: AccountSummary; busy: boolean; error: string; onCancel: () => void; onConfirm: () => void }) {
  const ref = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = ref.current
    if (dialog && !dialog.open) dialog.showModal()
    return () => { if (dialog?.open) dialog.close() }
  }, [])
  return <dialog ref={ref} className="delete-account-dialog" role="alertdialog" aria-modal="true" aria-labelledby="delete-account-title" aria-describedby="delete-account-description" onCancel={(event) => { event.preventDefault(); if (!busy) onCancel() }} onMouseDown={(event) => { if (event.target === event.currentTarget && !busy) onCancel() }}>
    <div className="delete-confirm-content">
      <div className="delete-confirm-heading"><span><Trash2 size={21} /></span><div><h2 id="delete-account-title">确认删除账号</h2><p id="delete-account-description">将从本机清除加密认证信息和额度记录，此操作不会注销平台账号。</p></div></div>
      <strong className="delete-account-name">{account.alias}</strong>
      <small className="delete-account-meta">{account.email || account.provider} · {planLabel(account.plan || account.region)}</small>
      {error ? <div className="form-error delete-error" role="alert"><AlertTriangle size={17} /><span><strong>{error}</strong><small>账号信息仍然保留，你可以重试或取消。</small></span></div> : null}
    </div>
    <footer className="delete-confirm-actions"><button className="secondary-button" onClick={onCancel} disabled={busy} autoFocus>取消</button><button className="danger-button" onClick={onConfirm} disabled={busy}>{busy ? <LoaderCircle className="spin" size={17} /> : <Trash2 size={17} />}{busy ? '正在删除' : '确认删除'}</button></footer>
  </dialog>
}

function EmptyState({ title, detail }: { title: string; detail: string }) { return <div className="empty-state"><Boxes size={28} /><strong>{title}</strong><span>{detail}</span></div> }
function DashboardSkeleton() { return <div className="skeleton-grid">{Array.from({ length: 8 }).map((_, index) => <i key={index} />)}</div> }
function formatDateTime(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function formatCompactDate(value: string) { return new Date(value).toLocaleString('zh-CN', { hour12: false, month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) }
function planLabel(value?: string) { if (!value) return '未标注'; return value.toLowerCase() === 'plus' ? 'Plus' : value.toLowerCase() === 'pro' ? 'Pro' : value }
function capabilityLabel(value: string) { return ({ quota: '额度', usage: '用量', credits: '积分', balance: '余额', token_plan: 'Token Plan', checkin: '签到' } as Record<string, string>)[value] ?? value }
function authMethodLabel(value?: string) { return ({ credential_import: '认证文件导入', device_code: '官方设备登录', oauth: '官方网页登录', oauth_qr: '官方二维码登录', api_key: 'API Key', access_key: 'RAM AccessKey', cookie: '控制台 Cookie', session_token: '网页登录态' } as Record<string, string>)[value ?? ''] ?? (value || '未记录') }
function sameStringArray(left: readonly string[], right: readonly string[]) { return left.length === right.length && left.every((value, index) => value === right[index]) }
