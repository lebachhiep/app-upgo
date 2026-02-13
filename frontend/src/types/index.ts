export interface RelayStats {
  bytes_sent: number
  bytes_recv: number
  uptime: number
  connections: number
  total_streams: number
  reconnect_count: number
  active_streams: number
  connected_nodes: number
  timestamp: number
}

export interface RelayStatus {
  IsConnected: boolean
  DeviceId: string
  Stats: RelayStats | null
  Version: string
  PartnerId: string
  Proxies: string[]
}

export interface Config {
  partner_id: string
  discovery_url: string
  proxies: string[]
  verbose: boolean
  auto_start: boolean
  launch_on_startup: boolean
  log_level: string
}

export interface PlatformInfo {
  os: string
  arch: string
  library: string
  supported: boolean
}

export interface VersionInfo {
  app: string
  library: string
}

export interface ProxyStatus {
  url: string
  alive: boolean
  latency: number
  error: string
  protocol: string    // detected: socks5, http, https
  since: number       // unix timestamp when proxy went alive
  bytes_sent: number  // accumulated bytes sent through this proxy
  bytes_recv: number  // accumulated bytes received through this proxy
}
