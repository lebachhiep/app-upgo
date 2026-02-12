import { useEffect, useState, useCallback } from 'react'
import { Card, Input, Switch, Button, message, Tag, Row, Col, Descriptions, Tooltip } from 'antd'
import {
  SettingOutlined,
  GlobalOutlined,
  DeleteOutlined,
  PlusOutlined,
  InfoCircleOutlined,
  DesktopOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  LoadingOutlined,
  ThunderboltOutlined,
} from '@ant-design/icons'
import type { Config, RelayStatus, PlatformInfo, VersionInfo, ProxyStatus } from '@/types'
import { AppService } from '@/services/wails'

const { TextArea } = Input

const CARD: React.CSSProperties = { background: 'rgba(255,255,255,0.06)', borderRadius: 8, height: '100%' }
const HEAD: React.CSSProperties = { borderBottom: '1px solid rgba(255,255,255,0.1)', color: '#8aa39a', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.15em' }

interface SettingsProps {
  onNavigate?: (view: string) => void
}

function Settings({ onNavigate }: SettingsProps) {
  const [config, setConfig] = useState<Config | null>(null)
  const [proxyText, setProxyText] = useState('')
  const [status, setStatus] = useState<RelayStatus | null>(null)
  const [platformInfo, setPlatformInfo] = useState<PlatformInfo | null>(null)
  const [versionInfo, setVersionInfo] = useState<VersionInfo | null>(null)
  const [proxyStatuses, setProxyStatuses] = useState<Record<string, ProxyStatus>>({})
  const [checkingAll, setCheckingAll] = useState(false)

  const loadConfig = useCallback(async () => {
    try {
      const cfg = await AppService.GetConfig()
      if (cfg) setConfig(cfg as unknown as Config)
    } catch { /* */ }
  }, [])

  useEffect(() => { loadConfig() }, [loadConfig])

  useEffect(() => {
    const load = async () => {
      try {
        const [s, p, v] = await Promise.all([
          AppService.GetStatus(),
          AppService.GetPlatformInfo(),
          AppService.GetVersion(),
        ])
        if (s) setStatus(s)
        if (p) setPlatformInfo(p)
        if (v) setVersionInfo(v)
      } catch { /* */ }
    }
    load()
  }, [])

  const handleSaveField = async (key: string, value: string) => {
    try {
      await AppService.SetConfigValue(key, value)
      message.success(`${key} saved`)
      loadConfig()
    } catch (err) {
      message.error(`Failed to save ${key}: ${err}`)
    }
  }

  const handleToggle = async (key: string, checked: boolean) => {
    try {
      await AppService.SetConfigValue(key, String(checked))
      message.success(`${key} ${checked ? 'enabled' : 'disabled'}`)
      loadConfig()
    } catch (err) {
      message.error(`Failed to save: ${err}`)
    }
  }

  const handleAddProxies = async () => {
    const lines = proxyText.split('\n').map(l => l.trim()).filter(Boolean)
    if (lines.length === 0) return
    let added = 0
    for (const line of lines) {
      try {
        await AppService.AddProxy(line)
        added++
      } catch (err) {
        message.error(`Failed to add ${line}: ${err}`)
      }
    }
    if (added > 0) {
      message.success(`${added} proxy${added > 1 ? ' entries' : ''} added`)
      setProxyText('')
      loadConfig()
      onNavigate?.('dashboard')
    }
  }

  const handleRemoveProxy = async (url: string) => {
    try {
      await AppService.RemoveProxy(url)
      message.success('Proxy removed')
      setProxyStatuses(prev => {
        const next = { ...prev }
        delete next[url]
        return next
      })
      loadConfig()
    } catch (err) {
      message.error(`Failed to remove proxy: ${err}`)
    }
  }

  const handleCheckAll = async () => {
    setCheckingAll(true)
    // Mark all as checking
    const proxies = config?.proxies ?? []
    const checking: Record<string, ProxyStatus> = {}
    for (const p of proxies) {
      checking[p] = { url: p, alive: false, latency: 0, error: 'checking', protocol: '', since: 0, bytes_sent: 0, bytes_recv: 0 }
    }
    setProxyStatuses(checking)

    try {
      const results = await AppService.CheckAllProxies()
      if (results) {
        const map: Record<string, ProxyStatus> = {}
        for (const r of results) {
          map[r.url] = r
        }
        setProxyStatuses(map)
      } else {
        setProxyStatuses({})
      }
    } catch (err) {
      message.error(`Check failed: ${err}`)
      setProxyStatuses({})
    } finally {
      setCheckingAll(false)
    }
  }

  const handleCheckOne = async (proxyUrl: string) => {
    setProxyStatuses(prev => ({
      ...prev,
      [proxyUrl]: { url: proxyUrl, alive: false, latency: 0, error: 'checking', protocol: '', since: 0, bytes_sent: 0, bytes_recv: 0 },
    }))
    try {
      const result = await AppService.CheckProxy(proxyUrl)
      if (result) {
        setProxyStatuses(prev => ({ ...prev, [proxyUrl]: result }))
      }
    } catch (err) {
      setProxyStatuses(prev => ({
        ...prev,
        [proxyUrl]: { url: proxyUrl, alive: false, latency: 0, error: String(err), protocol: '', since: 0, bytes_sent: 0, bytes_recv: 0 },
      }))
    }
  }

  const handleRemoveAll = async () => {
    try {
      await AppService.RemoveAllProxies()
      message.success('All proxies removed')
      setProxyStatuses({})
      loadConfig()
    } catch (err) {
      message.error(`Failed to remove proxies: ${err}`)
    }
  }

  const proxyCount = config?.proxies?.length ?? 0

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      {/* Top row: General + Proxies side by side */}
      <Row gutter={[10, 10]} align="stretch">
        <Col xs={24} md={12}>
          <Card
            size="small"
            title={<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><SettingOutlined style={{ color: '#e0e0f0', fontSize: 12 }} />General</span>}
            style={CARD}
            headStyle={HEAD}
          >
            <SettingField
              label="Partner ID"
              description="Your unique partner identifier"
              value={config?.partner_id ?? ''}
              onSave={(v) => handleSaveField('partner_id', v)}
            />
            <SettingRow label="Launch on Startup" description="Auto-start node when system boots">
              <Switch
                size="small"
                checked={config?.launch_on_startup ?? false}
                onChange={async (checked) => {
                  try {
                    await AppService.SetLaunchOnStartup(checked)
                    message.success(`Launch on startup ${checked ? 'enabled' : 'disabled'}`)
                    loadConfig()
                  } catch (err) {
                    message.error(`Failed to save: ${err}`)
                  }
                }}
              />
            </SettingRow>
            <SettingRow label="Close to Tray" description="Minimize to system tray when closing">
              <Switch
                size="small"
                checked={config?.close_to_tray ?? false}
                onChange={(checked) => handleToggle('close_to_tray', checked)}
              />
            </SettingRow>
          </Card>
        </Col>
        <Col xs={24} md={12}>
          <Card
            size="small"
            style={{ ...CARD, overflow: 'hidden' }}
            styles={{ body: { padding: 0 } }}
          >
            {/* Custom header — avoids Ant Design Card title rendering issues */}
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '8px 12px', borderBottom: '1px solid rgba(255,255,255,0.1)' }}>
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 6, color: '#8aa39a', fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.15em' }}><GlobalOutlined style={{ color: '#e0e0f0', fontSize: 12 }} />Proxies</span>
              <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                <Button
                  size="small"
                  type="text"
                  icon={checkingAll ? <LoadingOutlined spin /> : <ThunderboltOutlined />}
                  onClick={handleCheckAll}
                  disabled={checkingAll || proxyCount === 0}
                  style={{ fontSize: 11, color: '#22edeb', padding: '0 6px', height: 22 }}
                >
                  Check All
                </Button>
                <Button
                  size="small"
                  type="text"
                  danger
                  icon={<DeleteOutlined />}
                  onClick={handleRemoveAll}
                  disabled={proxyCount === 0}
                  style={{ fontSize: 11, padding: '0 6px', height: 22 }}
                >
                  Delete All
                </Button>
              </div>
            </div>
            <div style={{ padding: '12px' }}>
            <div style={{ marginBottom: 10 }}>
              <TextArea
                rows={2}
                placeholder={"socks5://user:pass@host:port\nOne proxy per line"}
                value={proxyText}
                onChange={(e) => setProxyText(e.target.value)}
                style={{ fontFamily: "'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace", fontSize: 12 }}
              />
              <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: 6 }}>
                <Button
                  size="small"
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={handleAddProxies}
                  disabled={!proxyText.trim()}
                >
                  Add {proxyText.split('\n').filter(l => l.trim()).length > 1 ? 'All' : ''}
                </Button>
              </div>
            </div>

            {/* Proxy list — fixed height to prevent UI jumping */}
            <div style={{ height: 200, overflowY: 'auto' }}>
            {/* Proxy list header — always visible */}
            <div style={{ ...pxs.header, position: 'sticky', top: 0, zIndex: 1, background: 'rgba(255,255,255,0.06)' }}>
              <span style={{ ...pxs.col, minWidth: 20 }}>#</span>
              <span style={{ ...pxs.col, minWidth: 52 }}>Type</span>
              <span style={{ ...pxs.col, flex: 1 }}>Proxy</span>
              <span style={{ ...pxs.col, minWidth: 60 }}>Status</span>
              <span style={{ ...pxs.col, minWidth: 52 }}>Latency</span>
              <span style={{ minWidth: 56 }} />
            </div>

            {config?.proxies?.map((proxy, index) => {
              const st = proxyStatuses[proxy]
              const isChecking = st?.error === 'checking'
              const proto = st?.protocol ? st.protocol.toUpperCase() : ''

              return (
                <div key={index} style={pxs.row}>
                  <span style={{ ...pxs.cell, minWidth: 20, color: '#8aa39a' }}>{index + 1}</span>
                  <span style={{ ...pxs.cell, minWidth: 52 }}>
                    {proto ? (
                      <Tag style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }} color="blue">{proto}</Tag>
                    ) : (
                      <Tag style={{ margin: 0, fontSize: 10, lineHeight: '16px', padding: '0 4px' }} color="default">—</Tag>
                    )}
                  </span>
                  <Tooltip title={proxy}>
                    <span style={{ ...pxs.cell, flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', color: '#e0e0f0' }}>
                      {proxy}
                    </span>
                  </Tooltip>
                  <span style={{ ...pxs.cell, minWidth: 60 }}>
                    {!st ? (
                      <Tag style={pxs.tag} color="default">—</Tag>
                    ) : isChecking ? (
                      <Tag style={pxs.tag} icon={<LoadingOutlined spin />} color="processing">...</Tag>
                    ) : st.alive ? (
                      <Tag style={pxs.tag} icon={<CheckCircleOutlined />} color="success">OK</Tag>
                    ) : (
                      <Tooltip title={st.error}>
                        <Tag style={pxs.tag} icon={<CloseCircleOutlined />} color="error">Fail</Tag>
                      </Tooltip>
                    )}
                  </span>
                  <span style={{ ...pxs.cell, minWidth: 52, fontSize: 10, color: st?.alive ? '#52c41a' : '#8aa39a', fontFamily: "'SF Mono', monospace" }}>
                    {st && !isChecking ? `${st.latency}ms` : '—'}
                  </span>
                  <span style={{ display: 'flex', gap: 2, minWidth: 56 }}>
                    <Tooltip title="Test this proxy">
                      <Button
                        type="text"
                        size="small"
                        icon={isChecking ? <LoadingOutlined spin /> : <ThunderboltOutlined />}
                        onClick={() => handleCheckOne(proxy)}
                        disabled={isChecking}
                        style={{ color: '#22edeb', padding: '0 4px', height: 22, width: 24 }}
                      />
                    </Tooltip>
                    <Tooltip title="Remove">
                      <Button
                        type="text"
                        danger
                        size="small"
                        icon={<DeleteOutlined />}
                        onClick={() => handleRemoveProxy(proxy)}
                        style={{ padding: '0 4px', height: 22, width: 24 }}
                      />
                    </Tooltip>
                  </span>
                </div>
              )
            })}

            {proxyCount === 0 && (
              <div style={{ padding: '10px 0', fontSize: 12, color: '#8aa39a', textAlign: 'center' }}>
                No proxies configured
              </div>
            )}
            </div>
            </div>
          </Card>
        </Col>
      </Row>

      {/* Bottom row: Connection + System side by side */}
      <Row gutter={[10, 10]} align="stretch">
        <Col xs={24} md={12}>
          <Card
            size="small"
            title={<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><InfoCircleOutlined style={{ color: '#e0e0f0', fontSize: 12 }} />Connection</span>}
            style={CARD}
            headStyle={HEAD}
          >
            <Descriptions column={1} size="small" colon={false}>
              <Descriptions.Item label="Partner ID">{status?.PartnerId || '—'}</Descriptions.Item>
              <Descriptions.Item label="Device ID">{status?.DeviceId || '—'}</Descriptions.Item>
              <Descriptions.Item label="Library">{status?.Version || '—'}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
        <Col xs={24} md={12}>
          <Card
            size="small"
            title={<span style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}><DesktopOutlined style={{ color: '#e0e0f0', fontSize: 12 }} />System</span>}
            style={CARD}
            headStyle={HEAD}
          >
            <Descriptions column={1} size="small" colon={false}>
              <Descriptions.Item label="Platform">{platformInfo ? `${platformInfo.os}/${platformInfo.arch}` : '—'}</Descriptions.Item>
              <Descriptions.Item label="Native Library">{platformInfo?.library || '—'}</Descriptions.Item>
              <Descriptions.Item label="Support">
                {platformInfo?.supported
                  ? <Tag color="success">Supported</Tag>
                  : <Tag color="error">Unsupported</Tag>
                }
              </Descriptions.Item>
              <Descriptions.Item label="App Version">{versionInfo?.app || '—'}</Descriptions.Item>
              <Descriptions.Item label="Lib Version">{versionInfo?.library || '—'}</Descriptions.Item>
            </Descriptions>
          </Card>
        </Col>
      </Row>
    </div>
  )
}

interface SettingRowProps { label: string; description: string; children: React.ReactNode }

function SettingRow({ label, description, children }: SettingRowProps) {
  return (
    <div style={row.row}>
      <div style={row.label}>
        <span style={row.name}>{label}</span>
        <span style={row.desc}>{description}</span>
      </div>
      {children}
    </div>
  )
}

interface SettingFieldProps { label: string; description: string; value: string; placeholder?: string; onSave: (value: string) => void }

function SettingField({ label, description, value, placeholder, onSave }: SettingFieldProps) {
  const [localValue, setLocalValue] = useState(value)
  useEffect(() => { setLocalValue(value) }, [value])
  const save = () => { if (localValue !== value) onSave(localValue) }

  return (
    <div style={row.row}>
      <div style={row.label}>
        <span style={row.name}>{label}</span>
        <span style={row.desc}>{description}</span>
      </div>
      <Input
        size="small"
        style={{ width: 180 }}
        value={localValue}
        placeholder={placeholder}
        onChange={(e) => setLocalValue(e.target.value)}
        onBlur={save}
        onPressEnter={save}
      />
    </div>
  )
}

const row: Record<string, React.CSSProperties> = {
  row: {
    display: 'flex',
    justifyContent: 'space-between',
    alignItems: 'center',
    padding: '8px 0',
    borderBottom: '1px solid rgba(255,255,255,0.1)',
    gap: 12,
  },
  label: {
    display: 'flex',
    flexDirection: 'column',
    gap: 1,
    flex: 1,
    minWidth: 0,
  },
  name: {
    fontSize: 13,
    fontWeight: 500,
    color: '#e0e0f0',
  },
  desc: {
    fontSize: 11,
    color: '#8aa39a',
  },
}

const pxs: Record<string, React.CSSProperties> = {
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '4px 0',
    borderBottom: '1px solid rgba(255,255,255,0.15)',
    fontSize: 9,
    fontWeight: 600,
    color: '#8aa39a',
    textTransform: 'uppercase',
    letterSpacing: '0.08em',
  },
  col: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
  },
  row: {
    display: 'flex',
    alignItems: 'center',
    gap: 6,
    padding: '5px 0',
    borderBottom: '1px solid rgba(255,255,255,0.06)',
    fontSize: 11,
  },
  cell: {
    overflow: 'hidden',
    textOverflow: 'ellipsis',
    whiteSpace: 'nowrap',
    fontFamily: "'SF Mono', Monaco, 'Cascadia Code', Consolas, monospace",
    fontSize: 11,
  },
  tag: {
    margin: 0,
    fontSize: 10,
    lineHeight: '16px',
    padding: '0 4px',
  },
}

export default Settings
