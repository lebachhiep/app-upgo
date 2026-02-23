import { Button } from 'antd'

interface Props {
  error: Error
  resetError: () => void
}

export default function ErrorFallback({ error, resetError }: Props) {
  return (
    <div style={{
      display: 'flex',
      flexDirection: 'column',
      alignItems: 'center',
      justifyContent: 'center',
      height: '100vh',
      background: '#142334',
      color: '#e0e0e0',
      padding: 24,
      textAlign: 'center',
    }}>
      <h2 style={{ color: '#ff6b6b', marginBottom: 8 }}>Something went wrong</h2>
      <p style={{ color: '#8aa39a', fontSize: 13, maxWidth: 400, marginBottom: 16 }}>
        {error.message || 'An unexpected error occurred.'}
      </p>
      <Button type="primary" onClick={resetError}>
        Try Again
      </Button>
    </div>
  )
}
