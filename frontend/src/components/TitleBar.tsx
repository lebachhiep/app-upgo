import { useCallback, useState } from 'react'
import { PlusOutlined, MinusOutlined, CopyOutlined, CheckOutlined } from '@ant-design/icons'
import { AppService, RuntimeService } from '@/services/wails'

interface TitleBarProps {
  deviceId?: string
  zoom: number
  onZoomIn: () => void
  onZoomOut: () => void
  onZoomReset: () => void
  isConnected?: boolean
  isRunning?: boolean
}

function TitleBar({ deviceId, zoom, onZoomIn, onZoomOut, onZoomReset, isConnected, isRunning }: TitleBarProps) {
  const [copied, setCopied] = useState(false)

  const handleClose = useCallback(() => {
    AppService.CloseWindow()
  }, [])

  const handleMinimise = useCallback(() => {
    RuntimeService.WindowMinimise()
  }, [])

  const handleCopyDeviceId = useCallback(() => {
    if (!deviceId) return
    navigator.clipboard.writeText(deviceId).then(() => {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
    }).catch(() => {})
  }, [deviceId])

  return (
    <div className="titlebar" style={styles.titleBar}>
      <div style={styles.left}>
        <div style={styles.brand}>
          <span style={styles.title}>UPGO.IO</span>
          <span style={{ display: 'inline-flex', alignItems: 'center', gap: 4, fontSize: 10, color: isConnected ? '#60B966' : isRunning ? '#FDC11C' : '#DD523C' }}>
            <span style={{ width: 6, height: 6, borderRadius: '50%', backgroundColor: isConnected ? '#60B966' : isRunning ? '#FDC11C' : '#DD523C' }} />
            {isConnected ? 'Connected' : isRunning ? 'Connecting' : 'Offline'}
          </span>
          {deviceId && (
            <span className="titlebar-nodrag" style={styles.deviceId} title={`Click to copy: ${deviceId}`}>
              <span style={styles.deviceLabel}>Device ID:</span> {deviceId}
              <span onClick={handleCopyDeviceId} style={styles.copyBtn}>
                {copied ? <CheckOutlined style={{ color: '#52c41a' }} /> : <CopyOutlined />}
              </span>
            </span>
          )}
        </div>
      </div>
      <div style={styles.right}>
        {/* Zoom controls */}
        <div className="titlebar-nodrag" style={styles.zoomGroup}>
          <button className="titlebar-btn" style={styles.zoomBtn} onClick={onZoomOut}><MinusOutlined /></button>
          <button className="titlebar-btn" style={styles.zoomLabel} onClick={onZoomReset}>{Math.round(zoom * 100)}%</button>
          <button className="titlebar-btn" style={styles.zoomBtn} onClick={onZoomIn}><PlusOutlined /></button>
        </div>
        {/* Window controls */}
        <div className="titlebar-nodrag" style={styles.controls}>
          <button className="titlebar-nodrag titlebar-btn" style={styles.btn} onClick={handleMinimise}>&#x2500;</button>
          <button className="titlebar-nodrag titlebar-btn" style={styles.btn} onClick={() => RuntimeService.WindowToggleMaximise()}>&#x2610;</button>
          <button className="titlebar-nodrag titlebar-close" style={styles.closeBtn} onClick={handleClose}>&#x2715;</button>
        </div>
      </div>
    </div>
  )
}

const styles: Record<string, React.CSSProperties> = {
  titleBar: {
    position: 'fixed',
    top: 0,
    left: 0,
    right: 0,
    height: 32,
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'space-between',
    backgroundColor: '#142334',
    borderBottom: '1px solid #1E344E',
    zIndex: 1000,
    userSelect: 'none',
  },
  left: {
    display: 'flex',
    alignItems: 'center',
    gap: 0,
    height: '100%',
    flex: 1,
    minWidth: 0,
  },
  right: {
    display: 'flex',
    alignItems: 'center',
    height: '100%',
    gap: 0,
  },
  brand: {
    display: 'flex',
    alignItems: 'center',
    gap: 10,
    paddingLeft: 14,
    flex: 1,
    height: '100%',
  },
  title: {
    fontSize: 13,
    color: '#e0e0f0',
    fontWeight: 600,
    letterSpacing: 0.5,
  },
  deviceLabel: {
    color: '#8aa39a',
  },
  deviceId: {
    fontSize: 10,
    color: '#bfc3d0',
    fontFamily: "'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace",
    letterSpacing: 0.3,
    display: 'inline-flex',
    alignItems: 'center',
    gap: 5,
  },
  copyBtn: {
    cursor: 'pointer',
    fontSize: 9,
    color: '#8B97A7',
    opacity: 0.6,
    transition: 'opacity 0.15s',
    display: 'inline-flex',
    alignItems: 'center',
  },
  zoomGroup: {
    display: 'flex',
    alignItems: 'center',
    height: '100%',
    marginRight: 4,
  },
  zoomBtn: {
    width: 28,
    height: 32,
    border: 'none',
    backgroundColor: 'transparent',
    color: '#bfc3d0',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    fontSize: 10,
    transition: 'background 0.15s ease',
  },
  zoomLabel: {
    minWidth: 36,
    height: 32,
    border: 'none',
    backgroundColor: 'transparent',
    color: '#bfc3d0',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    fontSize: 10,
    fontFamily: "'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace",
    transition: 'background 0.15s ease',
  },
  controls: {
    display: 'flex',
    height: '100%',
  },
  btn: {
    width: 46,
    height: 32,
    border: 'none',
    backgroundColor: 'transparent',
    color: '#bfc3d0',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    fontSize: 10,
    transition: 'background 0.15s ease',
  },
  closeBtn: {
    width: 46,
    height: 32,
    border: 'none',
    backgroundColor: 'transparent',
    color: '#bfc3d0',
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    cursor: 'pointer',
    fontSize: 10,
    transition: 'background 0.15s ease',
  },
}

export default TitleBar
