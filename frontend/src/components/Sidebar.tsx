import { Layout, Menu, Button } from 'antd'
import type { MenuProps } from 'antd'
import {
  DashboardOutlined,
  PoweroffOutlined,
  CaretRightOutlined,
} from '@ant-design/icons'

interface SidebarProps {
  currentView: string
  onViewChange: (view: string) => void
  isRunning: boolean
  isConnected: boolean
  onStart: () => void
  onStop: () => void
  collapsed: boolean
}

const menuItems: MenuProps['items'] = [
  { key: 'dashboard', icon: <DashboardOutlined />, label: 'Dashboard' },
]

function Sidebar({ currentView, onViewChange, isRunning, isConnected, onStart, onStop, collapsed }: SidebarProps) {
  const statusColor = isConnected ? '#4ade80' : isRunning ? '#fbbf24' : '#e0e0f099'
  const statusText = isConnected ? 'Online' : isRunning ? 'Connecting' : 'Offline'

  return (
    <Layout.Sider
      width={210}
      collapsedWidth={0}
      collapsed={collapsed}
      trigger={null}
      style={{
        height: '100%',
        borderRight: collapsed ? 'none' : '1px solid #1E344E',
        overflow: 'hidden',
      }}
    >
      <div style={styles.inner}>
        {/* Status */}
        <div style={styles.header}>
          <div style={{ flex: 1 }}>
            <div style={{ fontSize: 13, fontWeight: 600, color: '#e0e0f0' }}>UpGo Node</div>
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 5 }}>
            <div style={{ width: 6, height: 6, borderRadius: '50%', backgroundColor: statusColor }} />
            <span style={{ fontSize: 10, color: statusColor, fontWeight: 500 }}>{statusText}</span>
          </div>
        </div>

        {/* Navigation */}
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[currentView]}
          onClick={({ key }) => onViewChange(key)}
          items={menuItems}
          style={{ background: 'transparent', borderInlineEnd: 'none', flex: 1 }}
        />

        {/* Control */}
        <div style={styles.control}>
          {isRunning ? (
            <Button
              size="small"
              danger
              icon={<PoweroffOutlined />}
              onClick={onStop}
              block
              style={{ height: 36, borderRadius: 8, letterSpacing: '0.05em', textTransform: 'uppercase', fontSize: 12 }}
            >
              Stop
            </Button>
          ) : (
            <Button
              size="small"
              type="primary"
              icon={<CaretRightOutlined />}
              onClick={onStart}
              block
              style={{ height: 36, borderRadius: 8, letterSpacing: '0.05em', textTransform: 'uppercase', fontSize: 12 }}
            >
              Start Earning
            </Button>
          )}
        </div>
      </div>
    </Layout.Sider>
  )
}

const styles: Record<string, React.CSSProperties> = {
  inner: {
    display: 'flex',
    flexDirection: 'column',
    height: '100%',
  },
  header: {
    display: 'flex',
    alignItems: 'center',
    gap: 8,
    padding: '12px 14px',
    borderBottom: '1px solid #1E344E',
  },
  control: {
    padding: '10px 12px 14px',
    borderTop: '1px solid #1E344E',
  },
}

export default Sidebar
