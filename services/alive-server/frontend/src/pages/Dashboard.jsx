import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Card from '../components/Card.jsx'
import StatusDot from '../components/StatusDot.jsx'
import Button from '../components/Button.jsx'
import { useStatus } from '../App.jsx'

// ── Helpers ───────────────────────────────────────────────────────────

function relativeTime(iso) {
  if (!iso) return 'never'
  const t = new Date(iso).getTime()
  if (isNaN(t)) return 'unknown'
  const diffSec = Math.max(0, Math.floor((Date.now() - t) / 1000))
  if (diffSec < 60)         return `${diffSec}s ago`
  if (diffSec < 3600)       return `${Math.floor(diffSec / 60)} min ago`
  if (diffSec < 86400)      return `${Math.floor(diffSec / 3600)} h ago`
  if (diffSec < 86400 * 30) return `${Math.floor(diffSec / 86400)} d ago`
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
      sub: `${installed.length} services running · dotfiles in sync · domain ok`,
    }
  }
  const problems = []
  if (stopped.length > 0) problems.push(`${stopped.length} service${stopped.length > 1 ? 's' : ''} stopped`)
  if (!dotfilesOk)       problems.push('dotfiles ' + (dotfiles?.status || 'error'))
  if (!domainOk)         problems.push('domain expiring')

  return {
    color: stopped.length > 0 ? 'red' : 'yellow',
    label: problems.join(' · '),
    sub: 'check the cards below',
  }
}

// ── Claude login flow (inline) ────────────────────────────────────────

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
      <>
        <Button variant="ghost" onClick={start}>login to claude</Button>
        {msg && <div className={msg.error ? 'msg-error' : 'msg-success'}>{msg.text}</div>}
      </>
    )
  }
  if (state === 'waiting_url') {
    return <div className="muted" style={{ fontSize: '0.85rem' }}>starting login…</div>
  }
  return (
    <div className="login-box">
      <p>1. open this URL to authenticate:</p>
      <a href={authUrl} target="_blank" rel="noopener noreferrer">{authUrl}</a>
      <p style={{ marginTop: '0.8rem' }}>2. paste the code:</p>
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

// ── Service row (inside Services card) ───────────────────────────────

const DETAIL_ROUTES = {
  openclaw: '/services/details/openclaw',
  terminal: '/services/details/terminal',
  splitsies: '/services/details/splitsies',
  'david-s-checklist': '/services/details/david-s-checklist',
}

function ServiceRow({ svc, busy, onInstall, onUninstall }) {
  const detailRoute = DETAIL_ROUTES[svc.id]

  if (!svc.installed) {
    return (
      <div className="svc-row">
        <StatusDot color="grey" title="not installed" />
        <div className="svc-main">
          <span className="svc-name muted">{svc.name}</span>
          <span className="svc-path">{svc.path}</span>
        </div>
        <div className="svc-actions">
          <button className="link-btn" disabled={busy === svc.id} onClick={() => onInstall(svc.id)}>
            {busy === svc.id ? 'installing…' : 'install'}
          </button>
        </div>
      </div>
    )
  }
  return (
    <div className="svc-row">
      <StatusDot color={svc.running ? 'green' : 'red'} title={svc.running ? 'running' : 'stopped'} />
      <div className="svc-main">
        <span className="svc-name">{svc.name}</span>
        <span className="svc-path">{svc.path}</span>
      </div>
      <div className="svc-actions">
        <a href={svc.path} target="_blank" rel="noopener noreferrer">open</a>
        {detailRoute ? <Link to={detailRoute}>details</Link> : <span className="disabled">details</span>}
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

// ── Total effort card ─────────────────────────────────────────────────

function EffortCard() {
  const [data, setData] = useState(null)

  useEffect(() => {
    let cancelled = false
    fetch('/api/diary/effort')
      .then(r => r.json())
      .then(d => { if (!cancelled) setData(d) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [])

  if (!data || data.sessions === 0) return null

  return (
    <Card label="total effort · all sessions" className="full">
      <div className="effort-split">
        <div className="effort-col">
          <div className="card-headline">{data.total_hours}h</div>
          <div className="card-headline-sub">invested</div>
        </div>
        <div className="effort-col">
          <div className="card-headline">{data.sessions}</div>
          <div className="card-headline-sub">sessions</div>
        </div>
      </div>
    </Card>
  )
}

// ── System load card ──────────────────────────────────────────────────
// Shown first on the dashboard — CPU and memory are the two numbers
// you want to glance at before anything else. Headline percentages
// are big and color-coded by threshold; the sub-line gives context
// (load average vs cores for CPU, absolute GB for memory) so the %
// isn't hanging on its own.

function bytesToGB(b) {
  if (!b) return '0'
  return (b / 1024 / 1024 / 1024).toFixed(1)
}

function loadColor(pct) {
  if (pct >= 90) return 'var(--danger)'
  if (pct >= 70) return 'var(--warning)'
  return 'var(--brand)'
}

function LoadBar({ pct }) {
  return (
    <div className="load-bar" aria-hidden="true">
      <div
        className="load-bar-fill"
        style={{
          width: `${Math.max(2, Math.min(100, pct))}%`,
          background: loadColor(pct),
        }}
      />
    </div>
  )
}

function SystemLoadCard({ load }) {
  if (!load) {
    return (
      <Card label="system load · live" className="full">
        <div className="muted">loading…</div>
      </Card>
    )
  }
  const cpuPct = load.cpu_percent ?? 0
  const memPct = load.mem_percent ?? 0
  const cores = load.cpu_cores || 1
  const load1 = (load.load_avg_1 ?? 0).toFixed(2)
  const load5 = (load.load_avg_5 ?? 0).toFixed(2)
  const load15 = (load.load_avg_15 ?? 0).toFixed(2)
  const memUsed = bytesToGB(load.mem_used_bytes)
  const memTotal = bytesToGB(load.mem_total_bytes)

  return (
    <Card label="system load · live" className="full">
      <div className="load-split">
        <div className="load-col">
          <div className="load-headline" style={{ color: loadColor(cpuPct) }}>
            {cpuPct}%
          </div>
          <div className="load-lbl">cpu</div>
          <LoadBar pct={cpuPct} />
          <div className="load-meta">
            load {load1} · {load5} · {load15}
            <span className="muted"> / {cores} cores</span>
          </div>
        </div>
        <div className="load-col">
          <div className="load-headline" style={{ color: loadColor(memPct) }}>
            {memPct}%
          </div>
          <div className="load-lbl">memory</div>
          <LoadBar pct={memPct} />
          <div className="load-meta">
            {memUsed} / {memTotal} GB used
          </div>
        </div>
      </div>
    </Card>
  )
}

// ── Cloud spend card (combined total) ────────────────────────────────

// GCP on-demand hourly rate for e2-standard-4 in europe-west1.
const E2_STD4_HOURLY_USD = 0.1493

function CloudSpendCard() {
  const [data, setData] = useState(null)
  const [infra, setInfra] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const [spendRes, infraRes] = await Promise.all([
          fetch('/api/cloud-spend'),
          fetch('/api/services/infrastructure'),
        ])
        if (!spendRes.ok) throw new Error(`HTTP ${spendRes.status}`)
        const spendJson = await spendRes.json()
        const infraJson = infraRes.ok ? await infraRes.json() : null
        if (!cancelled) {
          setData(spendJson)
          setInfra(infraJson)
        }
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    }
    load()
    const t = setInterval(load, 60000)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

  const gcpBilled = data?.gcp_mtd_usd ?? 0
  const hasBillingData = gcpBilled > 0 && !data?.gcp_error
  const totalHours = (infra?.total_seconds_month ?? 0) / 3600
  const gcpEstimate = totalHours * E2_STD4_HOURLY_USD
  const gcpDisplay = hasBillingData ? gcpBilled : gcpEstimate
  const anthropic = data?.anthropic_mtd_usd ?? 0
  const total = gcpDisplay + anthropic

  return (
    <Card label="this month · cloud spend" className="full">
      {!data && !error && (
        <div className="muted">loading…</div>
      )}
      {data && (
        <>
          <div className="card-headline">{hasBillingData ? '' : '~'}${total.toFixed(2)}</div>
          <div className="card-headline-sub">
            month-to-date · google cloud + anthropic api
          </div>
          <div className="spend-split">
            <div className="spend-col">
              <div className="spend-amt">{hasBillingData ? '' : '~'}${gcpDisplay.toFixed(2)}</div>
              <div className="spend-lbl">google cloud</div>
              {hasBillingData ? (
                <div className="spend-meta">source: bigquery billing export</div>
              ) : (
                <div className="spend-meta">estimated from {totalHours.toFixed(0)}h vm runtime</div>
              )}
            </div>
            <div className="spend-col">
              <div className="spend-amt">${anthropic.toFixed(2)}</div>
              <div className="spend-lbl">anthropic api</div>
              {data.anthropic_error ? (
                <div className="spend-err" title={data.anthropic_error}>
                  data unavailable
                </div>
              ) : (
                <div className="spend-meta">source: cost_report api</div>
              )}
            </div>
          </div>
          <div className="card-actions">
            <Link to="/services/details/costs" className="btn btn-ghost">details</Link>
          </div>
        </>
      )}
    </Card>
  )
}

// ── Dashboard page ────────────────────────────────────────────────────

export default function Dashboard() {
  const { status, refresh, showToast } = useStatus()
  const [busy, setBusy] = useState(null)
  const [syncing, setSyncing] = useState(false)
  const [stopping, setStopping] = useState(false)

  if (!status) {
    return (
      <div className="loading">
        <StatusDot color="green" pulse />
        <span>loading…</span>
      </div>
    )
  }

  const hero = heroStatus(status)
  const { vm, user, claude, services, dotfiles, domain_expiry, system_load } = status

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

  const stopVM = async () => {
    const ok = confirm(
      'Stop the VM?\n\n' +
      'This will shut down the machine this dashboard runs on. ' +
      'The page will become unreachable in about 30 seconds. ' +
      'You will need to start the VM again from the GCP console ' +
      '(or `gcloud compute instances start`) before the dashboard ' +
      'comes back.\n\nContinue?'
    )
    if (!ok) return
    setStopping(true)
    showToast('requesting vm stop…', 'warn')
    try {
      const res = await fetch('/api/vm/stop', { method: 'POST' })
      const data = await res.json()
      if (data.success) {
        showToast('vm stop requested — dashboard will go dark shortly', 'warn')
      } else {
        showToast(data.error || 'stop failed', 'error')
        setStopping(false)
      }
    } catch (e) {
      showToast(e.message, 'error')
      setStopping(false)
    }
  }

  // dotfiles dot color
  let dotfilesColor = 'grey'
  if (dotfiles) {
    if (dotfiles.last_exit_status !== 0)    dotfilesColor = 'red'
    else if (dotfiles.status === 'behind')  dotfilesColor = 'yellow'
    else if (dotfiles.status === 'up-to-date') dotfilesColor = 'green'
  }

  return (
    <div className="detail-page">
      <div className="page-header">
        <div className="brand-title">attlas</div>
        {user?.email && (
          <div className="page-header-right">
            <span>{user.email}</span>
            <a href="/logout">logout</a>
          </div>
        )}
      </div>

      {/* Hero status line */}
      <div className="hero">
        <div className="hero-headline">
          <StatusDot color={hero.color} />
          <span>{hero.label}</span>
        </div>
        {hero.sub && <div className="hero-sub">{hero.sub}</div>}
      </div>

      {/* Card grid */}
      <div className="card-grid">
        {/* Total effort — topmost card, shows cumulative hours invested */}
        <EffortCard />

        {/* System load — full width, live CPU/memory */}
        <SystemLoadCard load={system_load} />

        {/* Cloud spend — full width */}
        <CloudSpendCard />

        {/* Infrastructure */}
        <Card label="infrastructure">
          <div className="card-row">
            <span className="k">name</span>
            <span className="v">{vm.name}</span>
          </div>
          <div className="card-row">
            <span className="k">machine</span>
            <span className="v">{vm.machine_type || '—'}</span>
          </div>
          <div className="card-row">
            <span className="k">zone</span>
            <span className="v">{vm.zone}</span>
          </div>
          <div className="card-row">
            <span className="k">external ip</span>
            <span className="v">{vm.external_ip}</span>
          </div>
          <div className="card-row">
            <span className="k">status</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color={stopping ? 'yellow' : 'green'} pulse={stopping} />
              {stopping ? 'stopping…' : 'running'}
            </span>
          </div>
          <div className="card-actions">
            <Link to="/services/details/infrastructure" className="btn btn-ghost">details</Link>
            <Button variant="danger" onClick={stopVM} disabled={stopping}>stop vm</Button>
          </div>
        </Card>

        {/* Status (domain + claude + dotfiles) */}
        <Card label="status">
          <div className="card-row">
            <span className="k">domain</span>
            <span className="v">
              <a href={`https://${vm.domain}/`}>{vm.domain}</a>
              {domain_expiry?.days_remaining !== undefined && (
                <span className="muted"> · {domain_expiry.days_remaining}d</span>
              )}
            </span>
          </div>
          <div className="card-row">
            <span className="k">claude</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color={claude?.authenticated ? 'green' : 'grey'} />
              {claude?.authenticated ? 'authenticated' : (claude?.installed ? 'not authenticated' : 'not installed')}
            </span>
          </div>
          <div className="card-row">
            <span className="k">dotfiles</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color={dotfilesColor} />
              {dotfiles ? (
                <>
                  <span>{dotfiles.status || 'unknown'}</span>
                  {dotfiles.head_commit && (
                    <span className="muted mono" style={{ fontSize: '0.8rem' }}>{dotfiles.head_commit}</span>
                  )}
                </>
              ) : 'unknown'}
            </span>
          </div>
          {dotfiles?.last_sync && (
            <div className="card-row">
              <span className="k">last sync</span>
              <span className="v">{relativeTime(dotfiles.last_sync)}</span>
            </div>
          )}
          <div className="card-actions">
            {claude?.installed && !claude?.authenticated ? (
              <ClaudeLogin onDone={refresh} />
            ) : (
              claude?.authenticated && (
                <a href="/logout" className="btn btn-ghost">claude logout</a>
              )
            )}
            <Button variant="ghost" onClick={syncDotfiles} disabled={syncing}>
              {syncing ? 'syncing…' : 'sync dotfiles'}
            </Button>
          </div>
        </Card>

        {/* Services — full width */}
        <Card label="services" className="full">
          <div className="svc-list">
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
        </Card>
      </div>
    </div>
  )
}
