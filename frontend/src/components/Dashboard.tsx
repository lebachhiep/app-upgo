import { useEffect, useState, useRef, useMemo, useCallback } from 'react'
import { Row, Col, Card, Tag, Button, Modal, Input, Switch, message } from 'antd'
import {
  ArrowUpOutlined,
  ArrowDownOutlined,
  CloudServerOutlined,
  FieldTimeOutlined,
  LoadingOutlined,
  WarningOutlined,
  PoweroffOutlined,
  CaretRightOutlined,
  SyncOutlined,
  CheckCircleOutlined,
  PlusOutlined,
  DeleteOutlined,
  EditOutlined,
} from '@ant-design/icons'
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid } from 'recharts'
import { AppService } from '@/services/wails'
import type { RelayStats, RelayStatus, ProxyStatus } from '@/types'

interface DashboardProps {
  status: RelayStatus | null
  stats: RelayStats | null
  isRunning: boolean
  isConnected: boolean
  libStatus?: { status: string; detail: string } | null
  onStart: (partnerId?: string) => void
  onStop: () => void
  hasPartnerId?: boolean
  proxyStatuses?: ProxyStatus[]
  directStats?: RelayStats | null
  partnerId: string
  onPartnerIdChange: (id: string) => void
}

interface ChartPoint { time: string; sent: number; recv: number }

const CARD: React.CSSProperties = { background: '#1E2D3E', borderRadius: 8 }
const CHART_SIZE = 60
const initChart = (): ChartPoint[] => {
  const now = new Date()
  return Array.from({ length: CHART_SIZE }, (_, i) => {
    const t = new Date(now.getTime() - (CHART_SIZE - 1 - i) * 2000)
    return { time: `${t.getMinutes()}:${t.getSeconds().toString().padStart(2, '0')}`, sent: 0, recv: 0 }
  })
}

function Dashboard({ status, stats, isRunning, isConnected, libStatus, onStart, onStop, hasPartnerId, proxyStatuses, directStats, partnerId, onPartnerIdChange }: DashboardProps) {
  const [chartData, setChartData] = useState<ChartPoint[]>(initChart)
  const prevStatsRef = useRef<{ sent: number; recv: number } | null>(null)
  const [localProxies, setLocalProxies] = useState<ProxyStatus[]>([])
  const [checkingIdx, setCheckingIdx] = useState<Set<number>>(new Set())
  const [checkingAll, setCheckingAll] = useState(false)
  const [now, setNow] = useState(() => Math.floor(Date.now() / 1000))
  const [showAddModal, setShowAddModal] = useState(false)
  const [proxyText, setProxyText] = useState('')
  const [launchOnStartup, setLaunchOnStartup] = useState(false)
  const [editingPid, setEditingPid] = useState(false)
  const [pidDraft, setPidDraft] = useState('')

  useEffect(() => {
    AppService.GetLaunchOnStartup().then(v => { if (v !== undefined) setLaunchOnStartup(v) }).catch(() => {})
  }, [])

  useEffect(() => {
    const timer = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 1000)
    return () => clearInterval(timer)
  }, [])

  // Reset chart and prev-stats reference when relay stops
  useEffect(() => {
    if (!isRunning) {
      setChartData(initChart())
      prevStatsRef.current = null
    }
  }, [isRunning])

  useEffect(() => {
    if (proxyStatuses) setLocalProxies(proxyStatuses)
  }, [proxyStatuses])

  const handleCheckOne = useCallback(async (idx: number, url: string) => {
    setCheckingIdx(prev => new Set(prev).add(idx))
    setLocalProxies(prev => prev.map((p, i) => i === idx ? { ...p, error: 'checking' } : p))
    try {
      const result = await AppService.CheckProxy(url)
      if (result) setLocalProxies(prev => prev.map((p, i) => i === idx ? result : p))
    } catch { /* */ }
    setCheckingIdx(prev => { const n = new Set(prev); n.delete(idx); return n })
  }, [])

  const handleCheckAll = useCallback(async () => {
    setCheckingAll(true)
    setLocalProxies(prev => prev.map(p => ({ ...p, error: 'checking' })))
    try {
      const results = await AppService.CheckAllProxies()
      if (results) setLocalProxies(results)
    } catch { /* */ }
    setCheckingAll(false)
  }, [])

  const handleAddProxies = useCallback(async () => {
    const lines = proxyText.split('\n').map(l => l.trim()).filter(Boolean)
    if (lines.length === 0) return
    let added = 0
    for (const line of lines) {
      try { await AppService.AddProxy(line); added++ } catch { /* */ }
    }
    if (added > 0) {
      message.success(`${added} proxy${added > 1 ? ' entries' : ''} added`)
      setProxyText('')
      setShowAddModal(false)
    }
  }, [proxyText])

  const handleRemoveProxy = useCallback(async (url: string) => {
    try {
      await AppService.RemoveProxy(url)
      message.success('Proxy removed')
    } catch { /* */ }
  }, [])

  const handleRemoveAll = useCallback(async () => {
    try {
      await AppService.RemoveAllProxies()
      message.success('All proxies removed')
    } catch { /* */ }
  }, [])

  const handleLaunchToggle = useCallback(async (checked: boolean) => {
    setLaunchOnStartup(checked)
    try { await AppService.SetLaunchOnStartup(checked) } catch { setLaunchOnStartup(!checked) }
  }, [])

  const handlePidEdit = useCallback(() => {
    setPidDraft(partnerId)
    setEditingPid(true)
  }, [partnerId])

  const handlePidSave = useCallback(async () => {
    const trimmed = pidDraft.trim()
    if (!trimmed) return
    try {
      await AppService.SetConfigValue('partner_id', trimmed)
      onPartnerIdChange(trimmed)
      message.success('Partner ID saved')
    } catch { message.error('Failed to save') }
    setEditingPid(false)
  }, [pidDraft, onPartnerIdChange])

  const handlePidCancel = useCallback(() => {
    setEditingPid(false)
  }, [])

  useEffect(() => {
    if (!stats) return
    const t = new Date()
    const timeLabel = `${t.getMinutes()}:${t.getSeconds().toString().padStart(2, '0')}`
    let deltaSent = 0, deltaRecv = 0
    if (prevStatsRef.current) {
      deltaSent = Math.max(0, stats.bytes_sent - prevStatsRef.current.sent)
      deltaRecv = Math.max(0, stats.bytes_recv - prevStatsRef.current.recv)
    }
    prevStatsRef.current = { sent: stats.bytes_sent, recv: stats.bytes_recv }
    setChartData(prev => [...prev, { time: timeLabel, sent: deltaSent, recv: deltaRecv }].slice(-CHART_SIZE))
  }, [stats])

  const fmtBytes = (b: number): string => {
    if (b === 0) return '0 B'
    const k = 1024, s = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(b) / Math.log(k))
    return `${parseFloat((b / Math.pow(k, i)).toFixed(1))} ${s[i]}`
  }

  const fmtUptime = (sec: number): string => {
    const h = Math.floor(sec / 3600), m = Math.floor((sec % 3600) / 60), s = sec % 60
    if (h > 0) return `${h}h ${m}m ${s}s`
    if (m > 0) return `${m}m ${s}s`
    return `${s}s`
  }

  // bytes per 2-second interval → Mbps (bytes * 8bits / 2sec / 1M)
  const toMbps = (bytes: number) => (bytes * 4 / 1_000_000).toFixed(2)
  // bytes per second → Mbps
  const bpsToMbps = (bps: number) => (bps * 8 / 1_000_000).toFixed(2)

  const { avgSent, avgRecv, curSent, curRecv } = useMemo(() => {
    const valid = chartData.filter(p => p.sent > 0 || p.recv > 0)
    const last = chartData[chartData.length - 1]
    return {
      avgSent: valid.length > 0 ? valid.reduce((s, p) => s + p.sent, 0) / valid.length : 0,
      avgRecv: valid.length > 0 ? valid.reduce((s, p) => s + p.recv, 0) / valid.length : 0,
      curSent: last?.sent ?? 0,
      curRecv: last?.recv ?? 0,
    }
  }, [chartData])

  const aliveCount = localProxies.filter(p => p.alive).length + (isRunning ? 1 : 0)
  const isDebug = status?.PartnerId === 'test'

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, height: '100%', minHeight: 0, overflow: 'hidden' }}>
      {/* Controls strip */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 6, flexWrap: 'wrap' }}>
        {/* Partner ID + Start/Stop */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
          <span style={{ fontSize: 10, color: '#8B97A7', whiteSpace: 'nowrap' }}>Partner ID</span>
          {!isRunning && (editingPid || !partnerId) ? (
            <input
              value={pidDraft}
              onChange={e => setPidDraft(e.target.value)}
              onKeyDown={e => { if (e.key === 'Enter' && pidDraft.trim()) { handlePidSave(); onStart(pidDraft.trim()) } if (e.key === 'Escape') handlePidCancel() }}
              placeholder="enter partner id"
              autoFocus={editingPid}
              onFocus={() => { if (!editingPid) { setPidDraft(partnerId); setEditingPid(true) } }}
              style={{ width: 150, height: 24, fontSize: 11, padding: '0 8px', background: '#111D2D', border: '1px solid #2D4F76', borderRadius: 4, color: '#e0e0f0', fontFamily: 'monospace', outline: 'none' }}
            />
          ) : (
            <div style={{ display: 'flex', alignItems: 'center', gap: 4, height: 24, padding: '0 8px', background: '#111D2D', borderRadius: 4, border: '1px solid #1E344E' }}>
              <span style={{ fontSize: 11, color: '#e0e0f0', fontFamily: 'monospace' }}>{partnerId}</span>
              {!isRunning && <EditOutlined onClick={handlePidEdit} style={{ fontSize: 9, color: '#8B97A7', cursor: 'pointer' }} />}
            </div>
          )}
          {isRunning ? (
            <Button size="small" danger icon={<PoweroffOutlined />} onClick={onStop} style={{ borderRadius: 6, fontSize: 12, height: 24 }}>Stop</Button>
          ) : (
            <Button size="small" type="primary" icon={<CaretRightOutlined />} onClick={() => { if (editingPid && pidDraft.trim()) { handlePidSave(); onStart(pidDraft.trim()) } else if (hasPartnerId) { onStart() } else if (pidDraft.trim()) { handlePidSave(); onStart(pidDraft.trim()) } }} disabled={!hasPartnerId && !pidDraft.trim()} style={{ borderRadius: 6, fontSize: 12, height: 24 }}>Start</Button>
          )}
        </div>

        {isDebug && <Tag color="gold" style={{ margin: 0 }}>DEBUG</Tag>}
        {libStatus && libStatus.status !== 'ready' && (
          <Tag icon={libStatus.status === 'checking' ? <LoadingOutlined spin /> : libStatus.status === 'error' ? <WarningOutlined /> : <CheckCircleOutlined />} color={libStatus.status === 'error' ? 'error' : 'processing'} style={{ margin: 0 }}>{libStatus.detail}</Tag>
        )}

        <div style={{ marginLeft: 'auto', display: 'flex', alignItems: 'center', gap: 5 }}>
          <span style={{ fontSize: 10, color: '#8B97A7' }}>Launch at Startup</span>
          <Switch size="small" checked={launchOnStartup} onChange={handleLaunchToggle} />
        </div>
      </div>

      {/* Stat cards */}
      <Row gutter={[8, 8]}>
        <Col xs={12} sm={6}>
          <Card size="small" style={CARD} bodyStyle={{ padding: '8px 10px' }}>
            <div style={st.statLabel}><ArrowUpOutlined style={st.statIcon} /><span>Upload</span></div>
            <div style={st.statValue}>{toMbps(curSent)} <span style={st.statUnit}>Mbps</span></div>
            <div style={st.statSub}>avg {toMbps(avgSent)} Mbps</div>
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small" style={CARD} bodyStyle={{ padding: '8px 10px' }}>
            <div style={st.statLabel}><ArrowDownOutlined style={st.statIcon} /><span>Download</span></div>
            <div style={st.statValue}>{toMbps(curRecv)} <span style={st.statUnit}>Mbps</span></div>
            <div style={st.statSub}>avg {toMbps(avgRecv)} Mbps</div>
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small" style={CARD} bodyStyle={{ padding: '8px 10px' }}>
            <div style={st.statLabel}><CloudServerOutlined style={st.statIcon} /><span>Nodes</span></div>
            <div style={st.statValue}>{stats?.connected_nodes ?? 0}</div>
            <div style={st.statSub}>{stats?.active_streams ?? 0} streams &middot; {stats?.total_streams ?? 0} total</div>
          </Card>
        </Col>
        <Col xs={12} sm={6}>
          <Card size="small" style={CARD} bodyStyle={{ padding: '8px 10px' }}>
            <div style={st.statLabel}><FieldTimeOutlined style={st.statIcon} /><span>Uptime</span></div>
            <div style={st.statValue}>{fmtUptime(stats?.uptime ?? 0)}</div>
            <div style={st.statSub}>{stats?.reconnect_count ?? 0} reconnects</div>
          </Card>
        </Col>
      </Row>

      {/* Chart + Bandwidth — side by side, no flex tricks */}
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        {/* Chart — fixed height */}
        <div style={{ flex: '1 1 380px', minWidth: 0, height: 200, background: '#1E2D3E', borderRadius: 8, overflow: 'hidden', border: '1px solid #1E344E', display: 'flex', flexDirection: 'column' }}>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '5px 12px', borderBottom: '1px solid #1E344E', flexShrink: 0 }}>
            <span style={{ color: '#8B97A7', fontSize: 10, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Throughput</span>
            <div style={{ display: 'flex', gap: 12 }}>
              <span style={{ fontSize: 10, color: '#22edeb', fontWeight: 500 }}><ArrowUpOutlined style={{ fontSize: 9 }} /> {toMbps(curSent)} Mbps</span>
              <span style={{ fontSize: 10, color: '#bfc3d0', fontWeight: 500 }}><ArrowDownOutlined style={{ fontSize: 9 }} /> {toMbps(curRecv)} Mbps</span>
            </div>
          </div>
          <div style={{ flex: 1, background: '#111D2D', minHeight: 0 }}>
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 6, right: 10, bottom: 2, left: 0 }}>
                <defs>
                  <linearGradient id="gSent" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#22edeb" stopOpacity={0.15} /><stop offset="100%" stopColor="#22edeb" stopOpacity={0.02} /></linearGradient>
                  <linearGradient id="gRecv" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stopColor="#bfc3d0" stopOpacity={0.15} /><stop offset="100%" stopColor="#bfc3d0" stopOpacity={0.02} /></linearGradient>
                </defs>
                <CartesianGrid stroke="#1E344E" strokeWidth={1} />
                <XAxis dataKey="time" tick={{ fill: '#8B97A7', fontSize: 9 }} axisLine={{ stroke: '#1E344E' }} tickLine={false} interval={9} />
                <YAxis tick={{ fill: '#8B97A7', fontSize: 9 }} axisLine={{ stroke: '#1E344E' }} tickLine={false} width={44} tickFormatter={fmtBytes} />
                <Tooltip contentStyle={{ backgroundColor: '#24374C', border: '1px solid #1E344E', borderRadius: 8, fontSize: 11, padding: '6px 10px' }} labelStyle={{ color: '#8B97A7', marginBottom: 4, fontSize: 10 }} formatter={(value: number, name: string) => [<span key={name} style={{ color: name === 'sent' ? '#22edeb' : '#bfc3d0' }}>{fmtBytes(value)} ({toMbps(value)} Mbps)</span>, name === 'sent' ? 'Send' : 'Receive']} />
                <Area type="monotone" dataKey="sent" stroke="#22edeb" fill="url(#gSent)" strokeWidth={1.5} dot={false} isAnimationActive={false} />
                <Area type="monotone" dataKey="recv" stroke="#bfc3d0" fill="url(#gRecv)" strokeWidth={1.5} dot={false} isAnimationActive={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', padding: '3px 12px', borderTop: '1px solid #1E344E', flexShrink: 0 }}>
            <div style={{ display: 'flex', gap: 14 }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 10, color: '#22edeb' }}><div style={{ width: 12, height: 2, backgroundColor: '#22edeb' }} /> Send</div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 5, fontSize: 10, color: '#bfc3d0' }}><div style={{ width: 12, height: 2, backgroundColor: '#bfc3d0' }} /> Receive</div>
            </div>
            <div style={{ fontSize: 9, color: '#8B97A7' }}>60s</div>
          </div>
        </div>

        {/* Bandwidth total panel */}
        <div style={{ flex: '0 0 160px', background: '#1E2D3E', borderRadius: 8, border: '1px solid #1E344E', display: 'flex', flexDirection: 'column' }}>
          <div style={{ padding: '5px 10px', borderBottom: '1px solid #1E344E' }}>
            <span style={{ color: '#8B97A7', fontSize: 10, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Total BW</span>
          </div>
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column', justifyContent: 'center', padding: '0 10px', gap: 8 }}>
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 2 }}><ArrowUpOutlined style={{ color: '#22edeb', fontSize: 10 }} /><span style={{ fontSize: 9, color: '#8B97A7', textTransform: 'uppercase', fontWeight: 500 }}>Sent</span></div>
              <div style={{ fontSize: 15, fontWeight: 700, color: '#e0e0f0', lineHeight: 1 }}>{fmtBytes(stats?.bytes_sent ?? 0)}</div>
            </div>
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 4, marginBottom: 2 }}><ArrowDownOutlined style={{ color: '#bfc3d0', fontSize: 10 }} /><span style={{ fontSize: 9, color: '#8B97A7', textTransform: 'uppercase', fontWeight: 500 }}>Received</span></div>
              <div style={{ fontSize: 15, fontWeight: 700, color: '#bfc3d0', lineHeight: 1 }}>{fmtBytes(stats?.bytes_recv ?? 0)}</div>
            </div>
            <div style={{ borderTop: '1px solid #1E344E', paddingTop: 6 }}>
              <span style={{ fontSize: 9, color: '#8B97A7', textTransform: 'uppercase', fontWeight: 500 }}>Combined</span>
              <div style={{ fontSize: 15, fontWeight: 700, color: '#e0e0f0', lineHeight: 1, marginTop: 2 }}>{fmtBytes((stats?.bytes_sent ?? 0) + (stats?.bytes_recv ?? 0))}</div>
            </div>
          </div>
        </div>
      </div>

      {/* Proxy table — always visible, scroll contained */}
      <div style={{ flex: 1, minHeight: 80, background: '#1E2D3E', borderRadius: 8, border: '1px solid #1E344E', overflow: 'hidden', display: 'flex', flexDirection: 'column' }}>
        {/* Header */}
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '5px 10px', borderBottom: '1px solid #1E344E', flexShrink: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span style={{ color: '#8B97A7', fontSize: 10, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Proxies</span>
            <Tag style={{ margin: 0, fontSize: 9, lineHeight: '14px', padding: '0 4px' }} color="success">{aliveCount} alive</Tag>
            <Tag style={{ margin: 0, fontSize: 9, lineHeight: '14px', padding: '0 4px' }} color="error">{localProxies.filter(p => !p.alive && p.error !== 'checking').length} dead</Tag>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 2 }}>
            <span onClick={() => setShowAddModal(true)} style={{ display: 'inline-flex', alignItems: 'center', gap: 3, fontSize: 9, color: '#22edeb', cursor: 'pointer', padding: '2px 6px', borderRadius: 4, transition: 'background .15s' }} onMouseEnter={e => (e.currentTarget.style.background = 'rgba(34,237,235,0.1)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}><PlusOutlined style={{ fontSize: 9 }} />Add</span>
            <span onClick={checkingAll || localProxies.length === 0 ? undefined : handleCheckAll} style={{ display: 'inline-flex', alignItems: 'center', gap: 3, fontSize: 9, color: '#8B97A7', cursor: checkingAll || localProxies.length === 0 ? 'default' : 'pointer', padding: '2px 6px', borderRadius: 4, opacity: checkingAll || localProxies.length === 0 ? 0.4 : 1, transition: 'background .15s' }} onMouseEnter={e => { if (!(checkingAll || localProxies.length === 0)) e.currentTarget.style.background = '#1E2D3E' }} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}><SyncOutlined spin={checkingAll} style={{ fontSize: 9 }} />Check</span>
            <span onClick={localProxies.length === 0 ? undefined : handleRemoveAll} style={{ display: 'inline-flex', alignItems: 'center', gap: 3, fontSize: 9, color: '#ff4d4f', cursor: localProxies.length === 0 ? 'default' : 'pointer', padding: '2px 6px', borderRadius: 4, opacity: localProxies.length === 0 ? 0.4 : 1, transition: 'background .15s' }} onMouseEnter={e => { if (localProxies.length > 0) e.currentTarget.style.background = 'rgba(255,77,79,0.08)' }} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}><DeleteOutlined style={{ fontSize: 9 }} />Clear</span>
          </div>
        </div>
        {/* Table area — horizontal scroll wrapper */}
        <div style={{ flex: 1, minHeight: 0, overflowX: 'auto', overflowY: 'hidden' }}>
        <div style={{ minWidth: 540, display: 'flex', flexDirection: 'column', height: '100%' }}>
        {/* Table head */}
        <div style={{ display: 'flex', alignItems: 'center', padding: '3px 10px', borderBottom: '1px solid #1A2F44', fontSize: 8, color: '#8B97A7', textTransform: 'uppercase', fontWeight: 600, letterSpacing: '0.05em', flexShrink: 0 }}>
          <span style={{ width: 18 }}>#</span>
          <span style={{ width: 46 }}>Type</span>
          <span style={{ flex: 1 }}>Proxy</span>
          <span style={{ width: 44, textAlign: 'right' }}>Ping</span>
          <span style={{ width: 60, textAlign: 'right' }}>Uptime</span>
          <span style={{ width: 52, textAlign: 'right' }}>Sent</span>
          <span style={{ width: 52, textAlign: 'right' }}>Recv</span>
          <span style={{ width: 50, textAlign: 'right' }}>Avg ↑</span>
          <span style={{ width: 50, textAlign: 'right' }}>Avg ↓</span>
          <span style={{ width: 34, textAlign: 'center' }}></span>
          <span style={{ width: 36 }}></span>
        </div>
        {/* Scrollable rows */}
        <div style={{ flex: 1, minHeight: 0, overflowY: 'auto' }}>
          {/* Direct row — always shown when relay is running */}
          {isRunning && (() => {
            const ds = directStats
            const dUp = ds?.uptime ?? 0
            const dAvgUp = dUp > 0 ? (ds?.bytes_sent ?? 0) / dUp : 0
            const dAvgDown = dUp > 0 ? (ds?.bytes_recv ?? 0) / dUp : 0
            return (
              <div style={{ display: 'flex', alignItems: 'center', padding: '4px 10px', borderBottom: '1px solid #172A3E', fontSize: 9 }}>
                <span style={{ width: 18, color: '#8B97A7', fontFamily: 'monospace' }}></span>
                <span style={{ width: 46 }}>
                  <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px', color: '#52c41a', borderColor: '#52c41a44', background: '#52c41a11' }}>DIRECT</Tag>
                </span>
                <span style={{ flex: 1, fontFamily: 'monospace', color: '#8B97A7' }}>No proxy</span>
                <span style={{ width: 44, textAlign: 'right', fontFamily: 'monospace', color: '#8B97A7' }}>-</span>
                <span style={{ width: 60, textAlign: 'right', fontFamily: 'monospace', color: '#22edeb' }}>{fmtUptime(dUp)}</span>
                <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: '#e0e0f0' }}>{fmtBytes(ds?.bytes_sent ?? 0)}</span>
                <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: '#bfc3d0' }}>{fmtBytes(ds?.bytes_recv ?? 0)}</span>
                <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: '#22edeb' }}>{bpsToMbps(dAvgUp)}</span>
                <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: '#bfc3d0' }}>{bpsToMbps(dAvgDown)}</span>
                <span style={{ width: 34, textAlign: 'center' }}>
                  {isConnected ? (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="success">OK</Tag>
                  ) : (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="warning">...</Tag>
                  )}
                </span>
                <span style={{ width: 36 }}></span>
              </div>
            )
          })()}
          {/* Proxy rows */}
          {localProxies.map((ps, i) => {
            const isChecking = ps.error === 'checking' || checkingIdx.has(i)
            const upSec = isRunning && ps.alive && ps.since > 0 ? Math.max(0, now - ps.since) : 0
            const pColor = ps.protocol === 'socks5' ? '#a78bfa' : ps.protocol === 'https' ? '#22edeb' : ps.protocol === 'http' ? '#faad14' : '#8B97A7'
            // Per-proxy avg speed: bytes / uptime
            const pAvgUp = upSec > 0 ? (ps.bytes_sent ?? 0) / upSec : 0
            const pAvgDown = upSec > 0 ? (ps.bytes_recv ?? 0) / upSec : 0

            return (
              <div key={i} style={{ display: 'flex', alignItems: 'center', padding: '4px 10px', borderBottom: '1px solid #172A3E', fontSize: 9 }}>
                <span style={{ width: 18, color: '#8B97A7', fontFamily: 'monospace' }}>{i + 1}</span>
                <span style={{ width: 46 }}>
                  {isChecking ? (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="processing">...</Tag>
                  ) : ps.protocol ? (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px', color: pColor, borderColor: pColor + '44', background: pColor + '11' }}>{ps.protocol.toUpperCase()}</Tag>
                  ) : (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="default">N/A</Tag>
                  )}
                </span>
                <span style={{ flex: 1, fontFamily: 'monospace', color: '#e0e0f0', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }} title={ps.url}>{ps.url}</span>
                <span style={{ width: 44, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#52c41a' : '#8B97A7' }}>
                  {isChecking ? '...' : `${ps.latency}ms`}
                </span>
                <span style={{ width: 60, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#22edeb' : '#8B97A7' }}>
                  {isChecking ? '...' : ps.alive ? fmtUptime(upSec) : '-'}
                </span>
                <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#e0e0f0' : '#8B97A7' }}>
                  {ps.alive ? fmtBytes(ps.bytes_sent ?? 0) : '-'}
                </span>
                <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#bfc3d0' : '#8B97A7' }}>
                  {ps.alive ? fmtBytes(ps.bytes_recv ?? 0) : '-'}
                </span>
                <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#22edeb' : '#8B97A7' }}>
                  {ps.alive ? `${bpsToMbps(pAvgUp)}` : '-'}
                </span>
                <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: ps.alive ? '#bfc3d0' : '#8B97A7' }}>
                  {ps.alive ? `${bpsToMbps(pAvgDown)}` : '-'}
                </span>
                <span style={{ width: 34, textAlign: 'center' }}>
                  {isChecking ? (
                    <LoadingOutlined spin style={{ fontSize: 10, color: '#1890ff' }} />
                  ) : ps.alive ? (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="success">OK</Tag>
                  ) : (
                    <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px' }} color="error">FAIL</Tag>
                  )}
                </span>
                <span style={{ width: 36, display: 'flex', justifyContent: 'flex-end', gap: 4, alignItems: 'center' }}>
                  <SyncOutlined spin={isChecking} onClick={isChecking ? undefined : () => handleCheckOne(i, ps.url)} style={{ fontSize: 9, color: '#8B97A7', cursor: isChecking ? 'default' : 'pointer', opacity: isChecking ? 0.5 : 0.6, transition: 'opacity .15s' }} onMouseEnter={e => { if (!isChecking) e.currentTarget.style.opacity = '1' }} onMouseLeave={e => { if (!isChecking) e.currentTarget.style.opacity = '0.6' }} />
                  <DeleteOutlined onClick={() => handleRemoveProxy(ps.url)} style={{ fontSize: 9, color: '#ff4d4f', cursor: 'pointer', opacity: 0.5, transition: 'opacity .15s' }} onMouseEnter={e => (e.currentTarget.style.opacity = '1')} onMouseLeave={e => (e.currentTarget.style.opacity = '0.5')} />
                </span>
              </div>
            )
          })}
          {/* Empty state — just spacer when no rows */}
          {!isRunning && localProxies.length === 0 && (
            <div style={{ padding: '8px 10px', textAlign: 'center', fontSize: 9, color: '#8B97A744' }}>—</div>
          )}
        </div>
        {/* Total row — always visible, pinned at bottom */}
        <div style={{ display: 'flex', alignItems: 'center', padding: '4px 10px', borderTop: '1px solid #1E344E', fontSize: 9, background: '#162638', flexShrink: 0 }}>
          <span style={{ width: 18 }}></span>
          <span style={{ width: 46 }}>
            <Tag style={{ margin: 0, fontSize: 7, lineHeight: '12px', padding: '0 3px', color: '#e0e0f0', borderColor: '#e0e0f044', background: '#e0e0f011' }}>TOTAL</Tag>
          </span>
          <span style={{ flex: 1, fontFamily: 'monospace', color: '#8B97A7' }}>{aliveCount > 0 ? `${aliveCount} proxies active` : '—'}</span>
          <span style={{ width: 44 }}></span>
          <span style={{ width: 60, textAlign: 'right', fontFamily: 'monospace', color: '#22edeb' }}>{fmtUptime(stats?.uptime ?? 0)}</span>
          <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: '#e0e0f0', fontWeight: 600 }}>{fmtBytes(stats?.bytes_sent ?? 0)}</span>
          <span style={{ width: 52, textAlign: 'right', fontFamily: 'monospace', color: '#bfc3d0', fontWeight: 600 }}>{fmtBytes(stats?.bytes_recv ?? 0)}</span>
          <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: '#22edeb' }}>{stats?.uptime ? bpsToMbps((stats.bytes_sent ?? 0) / stats.uptime) : '0.00'}</span>
          <span style={{ width: 50, textAlign: 'right', fontFamily: 'monospace', color: '#bfc3d0' }}>{stats?.uptime ? bpsToMbps((stats.bytes_recv ?? 0) / stats.uptime) : '0.00'}</span>
          <span style={{ width: 34 }}></span>
          <span style={{ width: 36 }}></span>
        </div>
        </div>
        </div>
      </div>

      {/* Add Proxies Modal */}
      <Modal
        title="Add Proxies"
        open={showAddModal}
        onCancel={() => setShowAddModal(false)}
        onOk={handleAddProxies}
        okText="Add"
        okButtonProps={{ disabled: !proxyText.trim() }}
        width={420}
      >
        <Input.TextArea
          rows={4}
          placeholder={"Supported formats (one per line):\nsocks5://user:pass@host:port\nhttp://user:pass@host:port\nhttps://host:port\nuser:pass@host:port\nhost:port:user:pass\nhost:port"}
          value={proxyText}
          onChange={(e) => setProxyText(e.target.value)}
          style={{ fontFamily: "'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace", fontSize: 12 }}
        />
      </Modal>
    </div>
  )
}

const st: Record<string, React.CSSProperties> = {
  statLabel: { fontSize: 10, color: '#8B97A7', fontWeight: 500, textTransform: 'uppercase', letterSpacing: '0.1em', marginBottom: 4, display: 'flex', alignItems: 'center', gap: 5, lineHeight: 1 },
  statIcon: { color: '#22edeb', fontSize: 11, lineHeight: 1 },
  statValue: { fontSize: 18, fontWeight: 700, color: '#e0e0f0', lineHeight: 1.2 },
  statUnit: { fontSize: 10, fontWeight: 400, color: '#8B97A7' },
  statSub: { fontSize: 10, color: '#8B97A7', marginTop: 1 },
}

export default Dashboard
