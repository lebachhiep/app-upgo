import React from 'react'
import ReactDOM from 'react-dom/client'
import * as Sentry from '@sentry/react'
import App from './App'
import ErrorFallback from './components/ErrorFallback'
import './styles/index.css'

Sentry.init({
  dsn: 'https://d7c05d7c2df2350ae859ed1b968dfaf0@o4510921798451200.ingest.de.sentry.io/4510934455812176',
  release: `app-upgo@${__APP_VERSION__}`,
  environment: import.meta.env.DEV ? 'development' : 'production',
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <Sentry.ErrorBoundary fallback={({ error, resetError }) => (
      <ErrorFallback error={error instanceof Error ? error : new Error(String(error))} resetError={resetError} />
    )}>
      <App />
    </Sentry.ErrorBoundary>
  </React.StrictMode>,
)
