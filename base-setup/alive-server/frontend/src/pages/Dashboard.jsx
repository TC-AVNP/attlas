import { useState } from 'react'
import { Link } from 'react-router-dom'
import StatusDot from '../components/StatusDot.jsx'
import Button from '../components/Button.jsx'
import { useStatus } from '../App.jsx'

// ── Helpers ───────────────────────────────────────────────────────────

function relativeTime(iso) {
  if (!iso) return 'never'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return 'unknown'
  const diffSec = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (diffSec < 60)           return `${diffSec}s ago`
  if (diffSec < 3600)         return `${Math.floor(diffSec / 60)} min ago`
  if (diffSec < 86400)        return `${Math.floor(diffSec / 3600)} h ago`
  if (diffSec < 86400 * 30)   return `${Math.floor(diffSec / 86400)} d ago`
  return new Date(iso).toISOString().slice(0, 10)
}

function heroStatus(data) {
  if (!data) return { color: 'grey', label: 'loading…', sub: '' }

  const services = data.services || []
  const installed = services.filter(s => s.installed)
  const stopped   = installed.filter(s => !s.running)

  const dotfiles = data.dotfiles
  const dotfilesOk = !dotfiles || (dotfiles.status === 'up-to-date' && dotfiles.last_exit_status === 0)

  const domainOk = !data.domain_expiry || data.domain_expiry.severity === 'ok' || data.domain_expiry.severity === 'unknown'

  if (stopped.length === 0 && dotfilesOk && domainOk) {
    return {
      color: 'green',
      label: 'all systems healthy',
      sub: `all ${installed.length} services running, dotfiles up to date`,
    }
  }
  const problems = []
  if (stopped.length > 0) problems.push(`${stopped.length} service${stopped.length > 1 ? 's' : ''} stopped`)
  if (!dotfilesOk)       problems.push('dotfiles ' + (dotfiles?.status || 'error'))
  if (!domainOk)         problems.push('domain expiring')

  return {
    color: stopped.length > 0 ? 'red' : 'yellow',
    label: problems.join(' · '),
    sub: 'check sections below',
  }
}

// ── Claude login inline flow ──────────────────────────────────────────

function ClaudeLogin({ onDone }) {
  const [state, setState] = useState('idle')
  const [authUrl, setAuthUrl] = useState('')
  const [code, setCode] = useState('')
  const [msg, setMsg] = useState(null)
  const { showToast } = useStatus()

  const start = async () => {
    setState('waiting_url')
    setMsg(null)
    try {
      const res = await fetch('/api/claude-login', { method: 'POST' })
      const data = await res.json()
      if (data.url) {
        setAuthUrl(data.url)
        setState('waiting_code')
      } else {
        setMsg({ text: data.error || 'Failed to start login', error: true })
        setState('idle')
      }
    } catch (e) {
      setMsg({ text: e.message, error: true })
      setState('idle')
    }
  }

  const submit = async () => {
    if (!code.trim()) return
    setMsg({ text: 'Submitting code…', error: false })
    try {
      const res = await fetch('/api/claude-login/code', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: code.trim() }),
      })
      const data = await res.json()
      if (data.success) {
        setMsg({ text: 'Logged in', error: false })
        setState('idle')
        setCode('')
        showToast('Claude Code authenticated', 'success')
        setTimeout(onDone, 800)
      } else {
        setMsg({ text: data.error || 'Login failed', error: true })
      }
    } catch (e) {
      setMsg({ text: e.message, error: true })
    }
  }

  if (state === 'idle') {
    return (
      <div>
        <Button variant="ghost" onClick={start}>login to claude</Button>
        {msg && <div className={msg.error ? 'msg-error' : 'msg-success'}>{msg.text}</div>}
      </div>
    )
  }
  if (state === 'waiting_url') {
    return <div className="muted" style={{ fontSize: '0.85rem' }}>starting login…</div>
  }
  return (
    <div className="login-box">
      <p>1. open this URL in a new tab to authenticate:</p>
      <a href={authUrl} target="_blank" rel="noopener noreferrer">{authUrl}</a>
      <p style={{ marginTop: '0.8rem' }}>2. paste the code from the browser:</p>
      <div className="row">
        <input
          className="input"
          type="text"
          placeholder="code#state"
          value={code}
          onChange={e => setCode(e.target.value)}
          onKeyDown={e => e.key === 'Enter' && submit()}
        />
        <Button onClick={submit}>submit</Button>
      </div>
      {msg && <div className={msg.error ? 'msg-error' : 'msg-success'}>{msg.text}</div>}
    </div>
  )
}

// ── Service row ───────────────────────────────────────────────────────

const DETAIL_ROUTES = {
  openclaw: '/services/details/openclaw',
}

function ServiceRow({ svc, busy, onInstall, onUninstall }) {
  const detailRoute = DETAIL_ROUTES[svc.id]

  if (!svc.installed) {
    return (
      <div className="service-row">
        <StatusDot color="grey" title="not installed" />
        <div className="name">
          <span className="muted">{svc.name}</span>
          <span className="path">{svc.path}</span>
        </div>
        <div className="actions">
          <button
            className="link-btn"
            disabled={busy === svc.id}
            onClick={() => onInstall(svc.id)}
          >
            {busy === svc.id ? 'installing…' : 'install'}
          </button>
        </div>
      </div>
    )
  }

  return (
    <div className="service-row">
      <StatusDot color={svc.running ? 'green' : 'red'} title={svc.running ? 'running' : 'stopped'} />
      <div className="name">
        <span>{svc.name}</span>
        <span className="path">{svc.path}</span>
      </div>
      <div className="actions">
        <a href={svc.path} target="_blank" rel="noopener noreferrer">open</a>
        {detailRoute ? (
          <Link to={detailRoute}>details</Link>
        ) : (
          <span className="disabled">details</span>
        )}
        <button
          className="link-btn dismiss"
          disabled={busy === svc.id}
          onClick={() => onUninstall(svc.id)}
          title={`uninstall ${svc.name}`}
        >
          ×
        </button>
      </div>
    </div>
  )
}

// ── Dashboard page ────────────────────────────────────────────────────

export default function Dashboard() {
  const { status, refresh, showToast } = useStatus()
  const [busy, setBusy] = useState(null)
  const [syncing, setSyncing] = useState(false)

  if (!status) {
    return (
      <div className="loading">
        <StatusDot color="green" pulse />
        <span>loading…</span>
      </div>
    )
  }

  const hero = heroStatus(status)
  const { vm, user, claude, services, dotfiles, domain_expiry } = status

  const installService = async (id) => {
    if (!confirm(`Install ${id}?`)) return
    setBusy(id)
    showToast(`installing ${id}…`, 'warn')
    try {
      const res = await fetch('/api/install-service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      })
      const data = await res.json()
      if (data.success) {
        showToast(`${id} installed`, 'success')
        setTimeout(refresh, 1000)
      } else {
        showToast(data.error || 'install failed', 'error')
      }
    } catch (e) { showToast(e.message, 'error') }
    finally { setBusy(null) }
  }

  const uninstallService = async (id) => {
    if (!confirm(`Uninstall ${id}? This will stop and remove the service.`)) return
    setBusy(id)
    showToast(`uninstalling ${id}…`, 'warn')
    try {
      const res = await fetch('/api/uninstall-service', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id }),
      })
      const data = await res.json()
      if (data.success) {
        showToast(`${id} uninstalled`, 'success')
        setTimeout(refresh, 1000)
      } else {
        showToast(data.error || 'uninstall failed', 'error')
      }
    } catch (e) { showToast(e.message, 'error') }
    finally { setBusy(null) }
  }

  const syncDotfiles = async () => {
    setSyncing(true)
    showToast('syncing dotfiles…', 'warn')
    try {
      const res = await fetch('/api/dotfiles/sync', { method: 'POST' })
      const data = await res.json()
      if (data.success) {
        showToast('dotfiles sync started', 'success')
        setTimeout(refresh, 2500)
      } else {
        showToast(data.error || 'sync failed', 'error')
      }
    } catch (e) { showToast(e.message, 'error') }
    finally { setSyncing(false) }
  }

  // dotfiles dot color
  let dotfilesColor = 'grey'
  if (dotfiles) {
    if (dotfiles.last_exit_status !== 0) dotfilesColor = 'red'
    else if (dotfiles.status === 'behind') dotfilesColor = 'yellow'
    else if (dotfiles.status === 'up-to-date') dotfilesColor = 'green'
  }

  return (
    <div className="page">
      <div className="page-header">
        <div className="brand-title">attlas</div>
        {user?.email && (
          <div className="page-header-right">
            <span>{user.email}</span>
            <a href="/logout">logout</a>
          </div>
        )}
      </div>

      {/* Hero */}
      <div className="hero">
        <div className="hero-headline">
          <StatusDot color={hero.color} />
          <span>{hero.label}</span>
        </div>
        {hero.sub && <div className="hero-sub">{hero.sub}</div>}
      </div>

      {/* Infrastructure section — VM identity lives on its own detail page */}
      <div className="section">
        <div className="section-label">infrastructure</div>
        <div className="status-rows">
          <div className="status-row">
            <span className="label">vm</span>
            <span className="value">
              {vm.name} · {vm.zone}
            </span>
            <span className="action">
              <Link to="/services/details/infrastructure">details</Link>
            </span>
          </div>
        </div>
      </div>

      {/* Status section */}
      <div className="section">
        <div className="section-label">status</div>
        <div className="status-rows">
          <div className="status-row">
            <span className="label">domain</span>
            <span className="value">
              <a href={`https://${vm.domain}/`}>{vm.domain}</a>
              {domain_expiry && domain_expiry.days_remaining !== undefined && (
                <span className="muted">
                  {'  →  renews in '}{domain_expiry.days_remaining} days
                </span>
              )}
            </span>
            <span />
          </div>
          <div className="status-row">
            <span className="label">claude</span>
            <span className="value" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', whiteSpace: 'normal' }}>
              <StatusDot color={claude?.authenticated ? 'green' : 'grey'} />
              {claude?.authenticated ? 'authenticated' : (claude?.installed ? 'not authenticated' : 'not installed')}
            </span>
            <span className="action">
              {claude?.authenticated && <a href="/logout">logout</a>}
              {claude?.installed && !claude?.authenticated && <ClaudeLogin onDone={refresh} />}
            </span>
          </div>
          <div className="status-row">
            <span className="label">dotfiles</span>
            <span className="value" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem', whiteSpace: 'normal' }}>
              <StatusDot color={dotfilesColor} />
              {dotfiles ? (
                <>
                  <span>{dotfiles.status || 'unknown'}</span>
                  {dotfiles.head_commit && (
                    <span className="muted mono" style={{ fontSize: '0.82rem' }}>· {dotfiles.head_commit}</span>
                  )}
                  {dotfiles.last_sync && (
                    <span className="muted" style={{ fontSize: '0.82rem' }}>· synced {relativeTime(dotfiles.last_sync)}</span>
                  )}
                </>
              ) : (
                <span className="muted">unknown</span>
              )}
            </span>
            <span className="action">
              <button
                className="link-btn"
                onClick={syncDotfiles}
                disabled={syncing}
                style={{ color: 'var(--accent)', background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.82rem' }}
              >
                {syncing ? 'syncing…' : 'sync now'}
              </button>
            </span>
          </div>
        </div>
      </div>

      {/* Services section */}
      <div className="section">
        <div className="section-label">services</div>
        <div className="service-rows">
          {services?.map(svc => (
            <ServiceRow
              key={svc.id}
              svc={svc}
              busy={busy}
              onInstall={installService}
              onUninstall={uninstallService}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
