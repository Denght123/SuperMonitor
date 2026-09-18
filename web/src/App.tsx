import { useEffect, useMemo, useRef, useState } from 'react'
import {
  Activity,
  AlertTriangle,
  Bell,
  Boxes,
  ChevronRight,
  CircleGauge,
  Command,
  Database,
  Languages,
  LayoutDashboard,
  Moon,
  RefreshCw,
  Search,
  Settings,
  ShieldCheck,
  Sun,
  TerminalSquare,
  X,
  Zap,
} from 'lucide-react'
import {
  Bar,
  CartesianGrid,
  Cell,
  ComposedChart,
  Line,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { AccountSummary, Alert, Overview, QuotaSignal } from './api/client'
import { useOverview } from './hooks/useOverview'
import { compactNumber, formatMetric, quotaValue, relativeTime } from './lib/format'

type Page = 'overview' | 'accounts' | 'usage' | 'activities' | 'alerts' | 'settings'
type Theme = 'dark' | 'light' | 'system'

const navItems: { id: Page; label: string; icon: typeof LayoutDashboard }[] = [
  { id: 'overview', label: '总览', icon: LayoutDashboard },
  { id: 'accounts', label: '账号池', icon: Boxes },
  { id: 'usage', label: '用量统计', icon: Activity },
  { id: 'activities', label: '活动中心', icon: Zap },
  { id: 'alerts', label: '告警', icon: Bell },
  { id: 'settings', label: '设置', icon: Settings },
]

const pageTitles: Record<Page, { title: string; description: string }> = {
  overview: { title: '监控总览', description: '所有数据均为合成演示数据，不包含真实账号或凭据。' },
  accounts: { title: '账号池', description: '按身份与服务绑定管理多个平台账号。' },
  usage: { title: '用量统计', description: '只聚合来源可信、单位可比较的真实用量。' },
  activities: { title: '活动中心', description: '签到默认关闭，启用前会展示接口来源与风险。' },
  alerts: { title: '告警中心', description: '额度、认证、刷新与活动异常集中处理。' },
  settings: { title: '系统设置', description: '安全、轮询、通知、语言与数据保留。' },
}

function useReducedMotion() {
  const [reduced, setReduced] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches)

  useEffect(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)')
    const listener = () => setReduced(media.matches)
    media.addEventListener('change', listener)
    return () => media.removeEventListener('change', listener)
  }, [])

  return reduced
}

export function App() {
  const [page, setPage] = useState<Page>('overview')
  const [theme, setTheme] = useState<Theme>('dark')
  const [selectedAccount, setSelectedAccount] = useState<AccountSummary | null>(null)
  const [searchOpen, setSearchOpen] = useState(false)
  const { data, loading, refreshing, error, streamStatus, lastEvent, refresh, retry } = useOverview()
  const commandKey = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘ K' : 'Ctrl K'

  useEffect(() => {
    const root = document.documentElement
    const resolved = theme === 'system' ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : theme
    root.dataset.theme = resolved
  }, [theme])

  useEffect(() => {
    const listener = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        setSearchOpen(true)
      }
    }
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [])

  const changePage = (next: Page) => {
    if (next === page) return
    if (document.startViewTransition) {
      document.startViewTransition(() => setPage(next))
    } else {
      setPage(next)
    }
  }

  const cycleTheme = () => setTheme((current) => (current === 'dark' ? 'light' : current === 'light' ? 'system' : 'dark'))

  return (
    <div className="app-shell">
      <aside className="sidebar" aria-label="主导航">
        <button className="brand" onClick={() => changePage('overview')} aria-label="返回总览">
          <span className="brand-mark"><CircleGauge size={21} /></span>
          <span className="brand-copy"><strong>SuperMonitor</strong><small>quota control plane</small></span>
        </button>
        <nav>
          {navItems.map((item) => {
            const Icon = item.icon
            return (
              <button key={item.id} className={page === item.id ? 'nav-item active' : 'nav-item'} onClick={() => changePage(item.id)} aria-current={page === item.id ? 'page' : undefined}>
                <Icon size={18} />
                <span>{item.label}</span>
                {item.id === 'alerts' && data?.alerts.length ? <b className="nav-count">{data.alerts.length}</b> : null}
              </button>
            )
          })}
        </nav>
        <div className="sidebar-foot">
          <div className="security-note"><ShieldCheck size={16} /><span>本地加密存储</span></div>
          <span>v0.1.0 · synthetic</span>
        </div>
      </aside>

      <main className="main-stage">
        <header className="topbar">
          <div className="environment-switch">
            <TerminalSquare size={16} />
            <span>本地部署</span>
            <b className={`connection-dot ${streamStatus}`} aria-label={`事件流 ${streamStatus}`} />
          </div>
          <div className="topbar-actions">
            <button className="icon-button search-trigger" onClick={() => setSearchOpen(true)} aria-label={`搜索（${commandKey}）`}><Search size={18} /><span>搜索</span><kbd>{commandKey}</kbd></button>
            <button className="icon-button" disabled aria-label="英文界面将在后续版本启用" title="英文界面将在后续版本启用"><Languages size={18} /><span>中</span></button>
            <button className="icon-button" onClick={cycleTheme} aria-label={`主题：${theme}`}>
              {theme === 'dark' ? <Moon size={18} /> : theme === 'light' ? <Sun size={18} /> : <Command size={18} />}
            </button>
            <button className="refresh-button" onClick={() => void refresh()} disabled={refreshing} aria-label={refreshing ? '正在刷新全部账号' : '刷新全部账号'}>
              <RefreshCw size={17} className={refreshing ? 'spin' : ''} />
              <span>{refreshing ? '刷新中' : '刷新全部'}</span>
            </button>
          </div>
        </header>

        <section className="page-heading">
          <div>
            <h1>{pageTitles[page].title}</h1>
            <p>{pageTitles[page].description}</p>
          </div>
          <div className="data-clock">
            <span>DATA CLOCK</span>
            <strong>{data ? new Date(data.generatedAt).toLocaleTimeString('zh-CN', { hour12: false }) : '--:--:--'}</strong>
          </div>
        </section>

        {error ? <ErrorBanner message={error} onRetry={() => void retry()} /> : null}
        {lastEvent ? <div className="event-toast" role="status"><Zap size={15} />{lastEvent}</div> : null}

        <div className="page-surface" key={page}>
          {loading || !data ? <DashboardSkeleton /> : <PageContent page={page} data={data} onSelectAccount={setSelectedAccount} onNavigate={changePage} />}
        </div>
      </main>

      {selectedAccount ? <AccountDrawer account={selectedAccount} onClose={() => setSelectedAccount(null)} /> : null}
      {searchOpen ? <CommandSearch data={data} onClose={() => setSearchOpen(false)} onNavigate={changePage} /> : null}
    </div>
  )
}

function PageContent({ page, data, onSelectAccount, onNavigate }: { page: Page; data: Overview; onSelectAccount: (account: AccountSummary) => void; onNavigate: (page: Page) => void }) {
  if (page === 'overview') return <OverviewPage data={data} onSelectAccount={onSelectAccount} onNavigate={onNavigate} />
  if (page === 'accounts') return <AccountsPage accounts={data.accounts} onSelectAccount={onSelectAccount} />
  if (page === 'usage') return <UsagePage data={data} />
  if (page === 'activities') return <ActivitiesPage accounts={data.accounts} />
  if (page === 'alerts') return <AlertsPage alerts={data.alerts} />
  return <SettingsPage />
}

function OverviewPage({ data, onSelectAccount, onNavigate }: { data: Overview; onSelectAccount: (account: AccountSummary) => void; onNavigate: (page: Page) => void }) {
  return (
    <>
      <KPIBand data={data} />
      <div className="chart-deck">
        <TokenChart data={data} />
        <ModelDonut data={data} />
      </div>
      <QuotaRail signals={data.quotaSignals} />
      <section className="account-section">
        <div className="section-header">
          <div><h2>账号信号</h2><p>按风险优先显示原生额度、来源和刷新状态</p></div>
          <button className="text-button" onClick={() => onNavigate('accounts')}>查看账号池 <ChevronRight size={15} /></button>
        </div>
        <AccountTable accounts={data.accounts} onSelectAccount={onSelectAccount} />
      </section>
    </>
  )
}

function KPIBand({ data }: { data: Overview }) {
  return (
    <section className="kpi-band" aria-label="关键指标">
      {data.kpis.map((kpi) => (
        <div className={`kpi-item tone-${kpi.tone}`} key={kpi.id}>
          <span>{kpi.label}</span>
          <strong>{formatMetric(kpi.value, kpi.unit)}</strong>
          <small>{kpi.delta > 0 ? `较前期 +${kpi.delta}%` : kpi.unit}</small>
        </div>
      ))}
      <div className="kpi-context">
        <Database size={17} />
        <div><strong>数据边界清晰</strong><span>积分、余额和订阅限额不混合汇总</span></div>
      </div>
    </section>
  )
}

function TokenChart({ data }: { data: Overview }) {
  const reducedMotion = useReducedMotion()
  return (
    <section className="instrument-panel token-panel">
      <div className="panel-header">
        <div><h2>30 天 Token 轨迹</h2><p>输入 / 输出 / 缓存 · 官方明细</p></div>
        <div className="panel-tools" aria-label="当前图表范围"><span className="filter-button active">30 天</span><span className="filter-button">全部平台</span></div>
      </div>
      <div className="legend"><span><i className="legend-input" />输入</span><span><i className="legend-output" />输出</span><span><i className="legend-cache" />缓存</span><span><i className="legend-requests" />请求数</span></div>
      <div className="chart-wrap signal-field">
        <ResponsiveContainer width="100%" height="100%">
          <ComposedChart data={data.tokenTrend} margin={{ top: 12, right: 4, bottom: 0, left: -14 }}>
            <CartesianGrid stroke="var(--chart-grid)" vertical={false} />
            <XAxis dataKey="date" tickFormatter={(value: string) => value.slice(5)} tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} minTickGap={28} />
            <YAxis yAxisId="tokens" tickFormatter={compactNumber} tick={{ fill: 'var(--text-muted)', fontSize: 11 }} axisLine={false} tickLine={false} />
            <YAxis yAxisId="requests" orientation="right" hide />
            <Tooltip content={<TokenTooltip />} cursor={{ fill: 'var(--hover-fill)' }} />
            <Bar yAxisId="tokens" dataKey="cacheTokens" stackId="tokens" fill="var(--chart-cache)" radius={[0, 0, 3, 3]} animationDuration={520} isAnimationActive={!reducedMotion} />
            <Bar yAxisId="tokens" dataKey="inputTokens" stackId="tokens" fill="var(--chart-input)" animationDuration={560} isAnimationActive={!reducedMotion} />
            <Bar yAxisId="tokens" dataKey="outputTokens" stackId="tokens" fill="var(--chart-output)" radius={[3, 3, 0, 0]} animationDuration={600} isAnimationActive={!reducedMotion} />
            <Line yAxisId="requests" type="monotone" dataKey="requests" stroke="var(--chart-line)" strokeWidth={1.5} dot={false} animationDuration={700} isAnimationActive={!reducedMotion} />
          </ComposedChart>
        </ResponsiveContainer>
      </div>
    </section>
  )
}

function TokenTooltip({ active, payload, label }: { active?: boolean; payload?: { name: string; value: number; color: string }[]; label?: string }) {
  if (!active || !payload) return null
  return <div className="chart-tooltip"><strong>{label}</strong>{payload.map((item) => <span key={item.name}><i style={{ background: item.color }} />{item.name}<b>{compactNumber(item.value)}</b></span>)}</div>
}

function ModelDonut({ data }: { data: Overview }) {
  const total = data.modelUsage.reduce((sum, item) => sum + item.tokens, 0)
  const reducedMotion = useReducedMotion()
  return (
    <section className="instrument-panel model-panel">
      <div className="panel-header"><div><h2>模型分布</h2><p>仅包含带模型字段的真实数据</p></div><span className="source-badge official">官方数据</span></div>
      <div className="donut-wrap">
        <ResponsiveContainer width="100%" height={220}>
          <PieChart>
            <Pie data={data.modelUsage} dataKey="tokens" nameKey="model" innerRadius={66} outerRadius={91} paddingAngle={2} stroke="none" animationDuration={620} isAnimationActive={!reducedMotion}>
              {data.modelUsage.map((entry) => <Cell key={entry.model} fill={entry.color} />)}
            </Pie>
            <Tooltip formatter={(value) => compactNumber(Number(value))} />
          </PieChart>
        </ResponsiveContainer>
        <div className="donut-total"><span>30D TOTAL</span><strong>{compactNumber(total)}</strong><small>tokens</small></div>
      </div>
      <div className="model-list">
        {data.modelUsage.map((item) => <div key={item.model}><i style={{ background: item.color }} /><span>{item.model}</span><b>{((item.tokens / total) * 100).toFixed(1)}%</b></div>)}
      </div>
    </section>
  )
}

function QuotaRail({ signals }: { signals: QuotaSignal[] }) {
  return (
    <section className="quota-rail" aria-label="额度窗口">
      <div className="quota-rail-label"><CircleGauge size={18} /><div><strong>额度信号</strong><span>按风险排序</span></div></div>
      <div className="quota-track">
        {signals.map((signal) => <QuotaCell key={signal.id} signal={signal} />)}
      </div>
    </section>
  )
}

function QuotaCell({ signal }: { signal: QuotaSignal }) {
  const percent = signal.remainingPercent ?? (signal.total ? (signal.value / signal.total) * 100 : undefined)
  return (
    <article className={`quota-cell status-${signal.status}`}>
      <div className="quota-cell-top"><span>{signal.provider}</span><i className="status-dot" /></div>
      <strong>{quotaValue(signal.value, signal.unit)}</strong>
      <p>{signal.label} · {signal.source}</p>
      {percent !== undefined ? <div className="quota-bar"><span style={{ transform: `scaleX(${Math.max(3, Math.min(percent, 100)) / 100})` }} /></div> : null}
      <small>{signal.resetAt ? `${relativeTime(signal.resetAt)}重置` : signal.expiresAt ? `${relativeTime(signal.expiresAt)}到期` : signal.confidence}</small>
    </article>
  )
}

function AccountTable({ accounts, onSelectAccount }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void }) {
  return (
    <div className="account-table" role="table" aria-label="账号信号列表">
      <div className="account-row account-head" role="row"><span>账号</span><span>服务绑定</span><span>当前额度</span><span>数据来源</span><span>刷新状态</span><span /></div>
      {accounts.map((account) => (
        <button className={`account-row status-${account.status}`} key={account.id} role="row" onClick={() => onSelectAccount(account)}>
          <span className="account-name"><i className="status-dot" /><span><strong>{account.alias}</strong><small>{account.provider} · {account.region}</small></span></span>
          <span className="service-tags">{account.services.map((service) => <em key={service}>{service}</em>)}</span>
          <span className="metric-cell"><strong>{account.primaryMetric}</strong><small>{account.secondaryMetric}</small></span>
          <span><em className="source-badge">{account.source}</em></span>
          <span className="refresh-cell"><strong>{relativeTime(account.lastRefreshedAt)}</strong><small>下次 {relativeTime(account.nextRefreshAt)}</small></span>
          <ChevronRight size={16} />
          {account.error ? <span className="inline-fault"><AlertTriangle size={15} /><strong>{account.error}</strong><em>打开详情处理</em></span> : null}
        </button>
      ))}
    </div>
  )
}

function AccountsPage({ accounts, onSelectAccount }: { accounts: AccountSummary[]; onSelectAccount: (account: AccountSummary) => void }) {
  return <section className="full-panel"><div className="section-header"><div><h2>全部身份与服务绑定</h2><p>{accounts.length} 个演示身份，按风险与更新时间排序</p></div><button className="primary-button" disabled title="OAuth 账号接入将在下一版本开放">添加账号</button></div><AccountTable accounts={accounts} onSelectAccount={onSelectAccount} /></section>
}

function UsagePage({ data }: { data: Overview }) {
  return <><KPIBand data={data} /><div className="chart-deck"><TokenChart data={data} /><ModelDonut data={data} /></div><section className="full-panel estimated-panel"><div><h2>估算消耗隔离区</h2><p>余额快照差值会在这里单独展示，不并入真实 Token 或模型统计。</p></div><span>当前无估算数据</span></section></>
}

function ActivitiesPage({ accounts }: { accounts: AccountSummary[] }) {
  const supported = accounts.filter((account) => account.services.some((service) => /Buddy/i.test(service)))
  return <section className="full-panel activity-panel"><div className="section-header"><div><h2>可执行活动</h2><p>自动签到默认关闭；手动执行会记录来源、结果和奖励。</p></div><button className="primary-button" disabled title="真实活动适配器接入后启用">全部签到</button></div>{supported.map((account) => <div className="activity-row" key={account.id}><div><Zap size={18} /><span><strong>{account.alias}</strong><small>{account.provider} · 今日未执行</small></span></div><span className="source-badge">社区适配</span><button className="secondary-button" disabled title="真实活动适配器接入后启用">立即签到</button></div>)}</section>
}

function AlertsPage({ alerts }: { alerts: Alert[] }) {
  return <section className="full-panel alert-list"><div className="section-header"><div><h2>未解决告警</h2><p>同一故障会自动去重，恢复后保留审计记录。</p></div><button className="secondary-button" disabled title="Webhook 配置将在后续版本开放">配置 Webhook</button></div>{alerts.map((alert) => <article key={alert.id} className={`alert-item severity-${alert.severity}`}><AlertTriangle size={20} /><div><span>{alert.provider} · {relativeTime(alert.createdAt)}</span><strong>{alert.title}</strong><p>{alert.message}</p><small>建议：{alert.recovery}</small></div><button className="secondary-button" disabled title="告警处理工作流将在后续版本开放">处理</button></article>)}</section>
}

function SettingsPage() {
  return <div className="settings-grid"><SettingBlock icon={ShieldCheck} title="安全与登录" detail="管理员密码 · TOTP · OIDC · 可信代理" status="待配置" /><SettingBlock icon={RefreshCw} title="轮询与保留" detail="后台 10–30 分钟 · 原始快照 90 天" status="默认策略" /><SettingBlock icon={Bell} title="通知通道" detail="站内告警 · HMAC Webhook" status="未连接" /><SettingBlock icon={Database} title="备份与迁移" detail="密码保护的完整加密备份" status="可用" /></div>
}

function SettingBlock({ icon: Icon, title, detail, status }: { icon: typeof Settings; title: string; detail: string; status: string }) {
  return <article className="setting-block"><Icon size={20} /><span><strong>{title}</strong><small>{detail}</small></span><em>{status}</em></article>
}

function AccountDrawer({ account, onClose }: { account: AccountSummary; onClose: () => void }) {
  const drawerRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const listener = (event: KeyboardEvent) => event.key === 'Escape' && onClose()
    window.addEventListener('keydown', listener)
    drawerRef.current?.focus()
    return () => window.removeEventListener('keydown', listener)
  }, [onClose])
  return <div className="drawer-layer" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><aside ref={drawerRef} tabIndex={-1} className="account-drawer" role="dialog" aria-modal="true" aria-label={`${account.alias} 详情`}><div className="drawer-head"><div><span>{account.provider} · {account.region}</span><h2>{account.alias}</h2></div><button className="icon-button" onClick={onClose} aria-label="关闭详情"><X size={19} /></button></div><div className={`drawer-status status-${account.status}`}><i className="status-dot" /><strong>{account.primaryMetric}</strong><span>{account.secondaryMetric}</span></div>{account.error ? <div className="drawer-error"><AlertTriangle size={18} /><div><strong>数据已过期</strong><p>{account.error}</p></div></div> : null}<section><h3>服务绑定</h3><div className="binding-list">{account.services.map((service) => <span key={service}><Boxes size={16} />{service}<em>已连接</em></span>)}</div></section><section><h3>采集状态</h3><dl><div><dt>数据来源</dt><dd>{account.source}</dd></div><div><dt>最后刷新</dt><dd>{relativeTime(account.lastRefreshedAt)}</dd></div><div><dt>下次刷新</dt><dd>{relativeTime(account.nextRefreshAt)}</dd></div><div><dt>凭据状态</dt><dd>已加密 · 演示</dd></div></dl></section><div className="drawer-actions"><button className="secondary-button" disabled title="历史快照视图将在后续版本开放">查看历史</button><button className="primary-button" disabled title="单账号刷新将随适配器开放">立即刷新</button></div></aside></div>
}

function CommandSearch({ data, onClose, onNavigate }: { data: Overview | null; onClose: () => void; onNavigate: (page: Page) => void }) {
  const [query, setQuery] = useState('')
  const results = useMemo(() => data?.accounts.filter((account) => `${account.alias}${account.provider}${account.services.join('')}`.toLowerCase().includes(query.toLowerCase())) ?? [], [data, query])
  useEffect(() => {
    const listener = (event: KeyboardEvent) => event.key === 'Escape' && onClose()
    window.addEventListener('keydown', listener)
    return () => window.removeEventListener('keydown', listener)
  }, [onClose])
  return <div className="command-layer" onMouseDown={(event) => event.target === event.currentTarget && onClose()}><div className="command-panel" role="dialog" aria-modal="true" aria-label="全局搜索"><div className="command-input"><Search size={19} /><input autoFocus aria-label="搜索账号、平台或服务" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索账号、平台或服务…" /><kbd>ESC</kbd></div><div className="command-results"><span>快速前往</span>{navItems.map((item) => <button key={item.id} onClick={() => { onNavigate(item.id); onClose() }}><item.icon size={16} />{item.label}<ChevronRight size={14} /></button>)}{query ? <><span>账号</span>{results.map((account) => <button key={account.id} onClick={() => { onNavigate('accounts'); onClose() }}><CircleGauge size={16} />{account.alias}<small>{account.provider}</small></button>)}</> : null}</div></div></div>
}

function ErrorBanner({ message, onRetry }: { message: string; onRetry: () => void }) {
  return <div className="error-banner" role="alert"><AlertTriangle size={18} /><div><strong>数据连接异常</strong><span>{message}</span></div><button onClick={onRetry}>重试</button></div>
}

function DashboardSkeleton() {
  return <div className="skeleton-stack" aria-label="正在加载监控数据"><div className="skeleton-band" /><div className="skeleton-grid"><div /><div /></div><div className="skeleton-rail" /><div className="skeleton-table" /></div>
}
