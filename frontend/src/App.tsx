import { useState, useEffect, useCallback, useRef } from 'react'
import { ConfigProvider, Modal, Input, Button, Space } from 'antd'
import { darkTheme } from './theme'
import { AppService, RuntimeService } from './services/wails'
import type { RelayStats, RelayStatus, ProxyStatus } from './types'
import TitleBar from './components/TitleBar'
import Dashboard from './components/Dashboard'

const ZOOM_STEP = 0.1
const ZOOM_MIN = 0.5
const ZOOM_MAX = 2.0

const TITLEBAR_HEIGHT = 32

function App() {
  const [isRunning, setIsRunning] = useState(false)
  const [isConnected, setIsConnected] = useState(false)
  const startingRef = useRef(false)
  const [savedPartnerId, setSavedPartnerId] = useState('')
  const [showStartDialog, setShowStartDialog] = useState(false)
  const [startPartnerId, setStartPartnerId] = useState('')
  const [status, setStatus] = useState<RelayStatus | null>(null)
  const [liveStats, setLiveStats] = useState<RelayStats | null>(null)
  const [zoom, setZoom] = useState(1.0)
  const [libStatus, setLibStatus] = useState<{ status: string; detail: string } | null>(null)
  const [proxyStatuses, setProxyStatuses] = useState<ProxyStatus[]>([])
  const [directStats, setDirectStats] = useState<RelayStats | null>(null)
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const zoomRef = useRef(1.0)

  const applyZoom = useCallback((factor: number) => {
    const clamped = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, Math.round(factor * 10) / 10))
    setZoom(clamped)
    zoomRef.current = clamped
  }, [])

  const handleZoomIn = useCallback(() => applyZoom(zoomRef.current + ZOOM_STEP), [applyZoom])
  const handleZoomOut = useCallback(() => applyZoom(zoomRef.current - ZOOM_STEP), [applyZoom])
  const handleZoomReset = useCallback(() => applyZoom(1.0), [applyZoom])

  // Keyboard zoom: Ctrl+=/-, Ctrl+0
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!e.ctrlKey && !e.metaKey) return
      if (e.key === '=' || e.key === '+') { e.preventDefault(); handleZoomIn() }
      else if (e.key === '-') { e.preventDefault(); handleZoomOut() }
      else if (e.key === '0') { e.preventDefault(); handleZoomReset() }
    }
    window.addEventListener('keydown', onKeyDown)
    return () => window.removeEventListener('keydown', onKeyDown)
  }, [handleZoomIn, handleZoomOut, handleZoomReset])

  // Ctrl+scroll zoom
  useEffect(() => {
    const onWheel = (e: WheelEvent) => {
      if (!e.ctrlKey && !e.metaKey) return
      e.preventDefault()
      if (e.deltaY < 0) handleZoomIn()
      else if (e.deltaY > 0) handleZoomOut()
    }
    window.addEventListener('wheel', onWheel, { passive: false })
    return () => window.removeEventListener('wheel', onWheel)
  }, [handleZoomIn, handleZoomOut])

  const fetchStatus = useCallback(async () => {
    try {
      const s = await AppService.GetStatus()
      if (s) {
        setStatus(s)
        setIsConnected(s.IsConnected)
        if (s.PartnerId) setSavedPartnerId(s.PartnerId)
        if (s.Stats) setLiveStats(s.Stats)
      }
      const running = await AppService.IsRelayRunning()
      // Don't override isRunning=true while relay is starting (proxy checks take time)
      if (running !== undefined && !startingRef.current) setIsRunning(running)
    } catch { /* */ }
  }, [])

  // Load saved config (partner ID + initial proxy list)
  useEffect(() => {
    const loadConfig = async () => {
      try {
        const cfg = await AppService.GetConfig()
        if (cfg) {
          if ((cfg as any).partner_id) setSavedPartnerId((cfg as any).partner_id)
          const proxies = (cfg as any).proxies as string[] | undefined
          if (proxies && proxies.length > 0) {
            setProxyStatuses(proxies.map(url => ({
              url, alive: false, latency: 0, error: '', protocol: '', since: 0, bytes_sent: 0, bytes_recv: 0,
            })))
          }
        }
      } catch { /* */ }
    }
    loadConfig()
  }, [])

  useEffect(() => {
    fetchStatus()
    pollRef.current = setInterval(fetchStatus, 2000)
    const cleanups: (() => void)[] = []

    const onStarted = RuntimeService.EventsOn('relay:started', () => {
      startingRef.current = false
      setIsRunning(true); fetchStatus()
    })
    if (onStarted) cleanups.push(onStarted)

    const onStopped = RuntimeService.EventsOn('relay:stopped', () => {
      setIsRunning(false); setIsConnected(false); setLiveStats(null); setDirectStats(null)
      setProxyStatuses(prev => prev.map(ps => ({ ...ps, bytes_sent: 0, bytes_recv: 0, since: 0 })))
      fetchStatus()
    })
    if (onStopped) cleanups.push(onStopped)

    const onStatus = RuntimeService.EventsOn('status:change', (c: unknown) => {
      setIsConnected(c as boolean)
    })
    if (onStatus) cleanups.push(onStatus)

    const onStats = RuntimeService.EventsOn('stats:update', (d: unknown) => {
      const s = d as RelayStats
      if (s) setLiveStats(s)
    })
    if (onStats) cleanups.push(onStats)

    const onLibStatus = RuntimeService.EventsOn('library:status', (d: unknown) => {
      const s = d as { status: string; detail: string }
      if (s) setLibStatus(s)
    })
    if (onLibStatus) cleanups.push(onLibStatus)

    const onProxyStatus = RuntimeService.EventsOn('proxy:status', (d: unknown) => {
      const s = d as ProxyStatus[]
      if (s) {
        setProxyStatuses(prev => {
          const incoming = new Map(s.map(p => [p.url, p]))
          // Only update existing entries — proxies:updated is the sole source of truth for which proxies exist
          return prev.map(p => incoming.get(p.url) ?? p)
        })
      }
    })
    if (onProxyStatus) cleanups.push(onProxyStatus)

    const onDirectStats = RuntimeService.EventsOn('direct:stats', (d: unknown) => {
      const s = d as RelayStats
      if (s) setDirectStats(s)
    })
    if (onDirectStats) cleanups.push(onDirectStats)

    // Sync proxy list changes from Settings → Dashboard
    const onProxiesUpdated = RuntimeService.EventsOn('proxies:updated', (d: unknown) => {
      const proxies = d as string[]
      if (proxies) {
        setProxyStatuses(prev => {
          const oldMap = new Map(prev.map(p => [p.url, p]))
          return proxies.map(url => oldMap.get(url) ?? {
            url,
            alive: false,
            latency: 0,
            error: '',
            protocol: '',
            since: 0,
            bytes_sent: 0,
            bytes_recv: 0,
          })
        })
      }
    })
    if (onProxiesUpdated) cleanups.push(onProxiesUpdated)

    return () => {
      if (pollRef.current) clearInterval(pollRef.current)
      cleanups.forEach(c => c())
    }
  }, [fetchStatus])

  const handleStart = async (overridePartnerId?: string) => {
    const pid = overridePartnerId || savedPartnerId
    if (pid) {
      try {
        startingRef.current = true
        setIsRunning(true)
        await AppService.StartRelay(pid)
        if (overridePartnerId) setSavedPartnerId(overridePartnerId)
      } catch (err) {
        console.error('Start failed:', err)
        setIsRunning(false)
      } finally {
        startingRef.current = false
      }
      return
    }
    // No partner_id at all — show dialog
    setStartPartnerId('')
    setShowStartDialog(true)
  }

  const handleDialogStart = async () => {
    if (!startPartnerId.trim()) return
    try {
      startingRef.current = true
      setIsRunning(true)
      await AppService.StartRelay(startPartnerId.trim())
      setSavedPartnerId(startPartnerId.trim())
      setShowStartDialog(false)
      setStartPartnerId('')
    } catch (err) {
      console.error('Start failed:', err)
      setIsRunning(false)
    } finally {
      startingRef.current = false
    }
  }

  const handleStop = async () => {
    try { await AppService.StopRelay() } catch { /* */ }
  }

  return (
    <ConfigProvider theme={darkTheme}>
      <div style={{ height: '100vh', display: 'flex', flexDirection: 'column', backgroundColor: '#142334', overflow: 'hidden' }}>
        <TitleBar
          deviceId={status?.DeviceId}
          zoom={zoom}
          onZoomIn={handleZoomIn}
          onZoomOut={handleZoomOut}
          onZoomReset={handleZoomReset}
          isConnected={isConnected}
          isRunning={isRunning}
        />

        {/* Zoomed content area — titlebar stays unzoomed */}
        <div style={{
          flex: 1,
          marginTop: TITLEBAR_HEIGHT,
          overflow: 'hidden',
          zoom: zoom,
          width: `${100 / zoom}vw`,
          height: `calc((100vh - ${TITLEBAR_HEIGHT}px) / ${zoom})`,
        }}>
          <div style={{ height: '100%', overflow: 'hidden', padding: '12px 14px', background: '#142334' }}>
            <Dashboard status={status} stats={liveStats} isRunning={isRunning} isConnected={isConnected} libStatus={libStatus} onStart={handleStart} onStop={handleStop} hasPartnerId={!!savedPartnerId} proxyStatuses={proxyStatuses} directStats={directStats} partnerId={savedPartnerId} onPartnerIdChange={setSavedPartnerId} />
          </div>
        </div>

        <Modal
          title="Start Node"
          open={showStartDialog}
          onCancel={() => setShowStartDialog(false)}
          footer={
            <Space>
              <Button onClick={() => setShowStartDialog(false)}>Cancel</Button>
              <Button type="primary" onClick={handleDialogStart} disabled={!startPartnerId.trim()}>Connect</Button>
            </Space>
          }
          width={420}
        >
          <p style={{ color: '#8aa39a', margin: '4px 0 16px', fontSize: 13, lineHeight: 1.5 }}>
            Enter your Partner ID to connect to the BNC network.
          </p>
          <Input
            placeholder="e.g. my-partner-id"
            value={startPartnerId}
            onChange={(e) => setStartPartnerId(e.target.value)}
            onPressEnter={handleDialogStart}
            autoFocus
          />
        </Modal>
      </div>
    </ConfigProvider>
  )
}

export default App
