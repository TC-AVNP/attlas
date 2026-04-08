import { useState, useEffect } from 'react'

function App() {
  const [vm, setVm] = useState(null)
  const [claude, setClaude] = useState(null)
  const [services, setServices] = useState([])
  const [toast, setToast] = useState(null)
  const [loginState, setLoginState] = useState('idle')
  const [authUrl, setAuthUrl] = useState('')
  const [checking, setChecking] = useState(false)
  const [loginMsg, setLoginMsg] = useState(null)
  const [installing, setInstalling] = useState(null)
  const [uninstalling, setUninstalling] = useState(null)

  const showToast = (msg, color) => {
    setToast({ msg, color })
    setTimeout(() => setToast(null), 4000)
  }

  const fetchStatus = async () => {
    try {
      const res = await fetch('/api/status')
      const data = await res.json()
      setVm(data.vm)
      setClaude(data.claude)
      setServices(data.services)
    } catch (e) {
      console.error('Failed to fetch status', e)
    }
  }

  useEffect(() => { fetchStatus() }, [])

  const startLogin = async () => {
    setLoginState('waiting_url')
    setLoginMsg(null)
    try {
      const res = await fetch('/api/claude-login', { method: 'POST' })
      const data = await res.json()
      if (data.url) {
        setAuthUrl(data.url)
        setLoginState('waiting_code')
      } else {
        setLoginMsg({ text: data.error || 'Failed to start login', error: true })
        setLoginState('idle')
      }
    } catch (e) {
      setLoginMsg({ text: e.message, error: true })
      setLoginState('idle')
    }
  }

  const checkAuth = async () => {
    setChecking(true)
    try {
      const res = await fetch('/api/claude-login/code', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({})
      })
      const data = await res.json()
      if (data.success) {
        setLoginMsg({ text: 'Logged in!', error: false })
        setLoginState('idle')
        setTimeout(fetchStatus, 1000)
      } else {
        setLoginMsg({ text: data.error || 'Not yet authenticated', error: true })
      }
    } catch (e) {
      setLoginMsg({ text: e.message, error: true })
    } finally {
      setChecking(false)
    }
  }

  const installService = async (id) => {
    if (!confirm(`Install ${id}?`)) return
    setInstalling(id)
    showToast(`Installing ${id}...`, '#5a67d8')
    try {
      const res = await fetch('/api/install-service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      })
      const data = await res.json()
      if (data.success) {
        showToast(`${id} installed!`, '#48bb78')
        setTimeout(fetchStatus, 1000)
      } else {
        showToast(data.error || 'Install failed', '#fc8181')
      }
    } catch (e) {
      showToast(e.message, '#fc8181')
    } finally {
      setInstalling(null)
    }
  }

  const uninstallService = async (id) => {
    if (!confirm(`Uninstall ${id}? This will stop and remove the service.`)) return
    setUninstalling(id)
    showToast(`Uninstalling ${id}...`, '#e53e3e')
    try {
      const res = await fetch('/api/uninstall-service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id })
      })
      const data = await res.json()
      if (data.success) {
        showToast(`${id} uninstalled`, '#48bb78')
        setTimeout(fetchStatus, 1000)
      } else {
        showToast(data.error || 'Uninstall failed', '#fc8181')
      }
    } catch (e) {
      showToast(e.message, '#fc8181')
    } finally {
      setUninstalling(null)
    }
  }

  if (!vm) return <p style={{ color: '#888', textAlign: 'center', marginTop: '3rem' }}>Loading...</p>

  return (
    <>
      <h1>I am alive!</h1>
      <div className="subtitle">Attlas VM Dashboard</div>

      <h2>VM Info</h2>
      <table>
        <tbody>
          <tr><td className="label">Name</td><td>{vm.name}</td></tr>
          <tr><td className="label">Zone</td><td>{vm.zone}</td></tr>
          <tr><td className="label">External IP</td><td>{vm.external_ip}</td></tr>
          <tr><td className="label">Domain</td><td><a href={`https://${vm.domain}/`}>{vm.domain}</a></td></tr>
        </tbody>
      </table>

      <h2>Claude Code</h2>
      {!claude?.installed ? (
        <div className="muted">Not installed. Run ~/attlas/base-setup/setup.sh</div>
      ) : claude?.authenticated ? (
        <div className="dot-green">Authenticated</div>
      ) : (
        <>
          <div className="dot-red">Not authenticated</div>
          {loginState === 'idle' && (
            <div style={{ marginTop: '0.8rem' }}>
              <button className="btn btn-primary" onClick={startLogin}>
                Login to Claude Code
              </button>
            </div>
          )}
          {loginState === 'waiting_url' && (
            <div style={{ marginTop: '0.8rem', color: '#888' }}>Starting login...</div>
          )}
          {loginState === 'waiting_code' && (
            <div className="login-box">
              <p>1. Click to authenticate:</p>
              <a href={authUrl} target="_blank" rel="noopener noreferrer" style={{ wordBreak: 'break-all' }}>
                {authUrl}
              </a>
              <div style={{ marginTop: '1rem' }}>
                <p>2. Complete login in the browser, then:</p>
                <button
                  className="btn btn-success"
                  onClick={checkAuth}
                  disabled={checking}
                  style={{ marginTop: '0.5rem' }}
                >
                  {checking ? 'Checking...' : 'I\'ve logged in — check status'}
                </button>
              </div>
            </div>
          )}
          {loginMsg && (
            <div className={loginMsg.error ? 'msg-error' : 'msg-success'} style={{ marginTop: '0.5rem' }}>
              {loginMsg.text}
            </div>
          )}
        </>
      )}

      <h2>Services</h2>
      <table>
        <tbody>
          <tr className="table-header">
            <td>Service</td><td>Status</td><td>Path</td>
          </tr>
          {services.map(svc => (
            <tr key={svc.id}>
              {svc.installed ? (
                <>
                  <td className={svc.running ? 'dot-green' : 'dot-red'}>{svc.name}</td>
                  <td>{svc.running ? 'running' : 'stopped'}</td>
                  <td>
                    {svc.path ? <a href={svc.path}>{svc.path}</a> : '\u2014'}
                    {' '}
                    <button
                      className="btn btn-uninstall"
                      onClick={() => uninstallService(svc.id)}
                      disabled={uninstalling === svc.id}
                      title="Uninstall"
                    >
                      {uninstalling === svc.id ? '...' : '\u2715'}
                    </button>
                  </td>
                </>
              ) : (
                <>
                  <td className="dot-grey muted">{svc.name}</td>
                  <td className="muted">not installed</td>
                  <td>
                    <button
                      className="btn btn-sm"
                      onClick={() => installService(svc.id)}
                      disabled={installing === svc.id}
                    >
                      {installing === svc.id ? 'Installing...' : 'Install'}
                    </button>
                  </td>
                </>
              )}
            </tr>
          ))}
        </tbody>
      </table>

      {toast && (
        <div className="toast" style={{ background: toast.color }}>
          {toast.msg}
        </div>
      )}
    </>
  )
}

export default App
