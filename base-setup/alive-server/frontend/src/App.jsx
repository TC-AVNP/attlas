import { useState, useEffect, useCallback, createContext, useContext } from 'react'
import { BrowserRouter, Routes, Route, Outlet, Link } from 'react-router-dom'
import Banner from './components/Banner.jsx'
import Dashboard from './pages/Dashboard.jsx'
import OpenclawDetail from './pages/detail/Openclaw.jsx'

// ── Shared status context ─────────────────────────────────────────────
// /api/status is the backend's firehose: vm, user, claude, services,
// dotfiles, domain_expiry. Every page consumes it via useStatus().

const StatusContext = createContext(null)

export function useStatus() {
  const ctx = useContext(StatusContext)
  if (!ctx) throw new Error('useStatus must be used within <StatusProvider>')
  return ctx
}

function StatusProvider({ children }) {
  const [status, setStatus] = useState(null)
  const [toast, setToast] = useState(null)

  const showToast = useCallback((msg, variant = 'success') => {
    setToast({ msg, variant })
    setTimeout(() => setToast(null), 4000)
  }, [])

  const fetchStatus = useCallback(async () => {
    try {
      const res = await fetch('/api/status')
      const data = await res.json()
      setStatus(data)
    } catch (e) {
      console.error('Failed to fetch status', e)
    }
  }, [])

  useEffect(() => { fetchStatus() }, [fetchStatus])

  return (
    <StatusContext.Provider value={{ status, refresh: fetchStatus, showToast }}>
      {children}
      {toast && (
        <div className={`toast toast-${toast.variant}`}>{toast.msg}</div>
      )}
    </StatusContext.Provider>
  )
}

// ── Shared layout ─────────────────────────────────────────────────────

function Layout() {
  const { status } = useStatus()

  return (
    <div className="layout">
      <Banner expiry={status?.domain_expiry} />
      <Outlet />
      <footer className="footer">
        attlas · <Link to="/">dashboard</Link>
      </footer>
    </div>
  )
}

// ── Router ────────────────────────────────────────────────────────────

export default function App() {
  return (
    <BrowserRouter>
      <StatusProvider>
        <Routes>
          <Route element={<Layout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/services/details/openclaw" element={<OpenclawDetail />} />
          </Route>
        </Routes>
      </StatusProvider>
    </BrowserRouter>
  )
}
