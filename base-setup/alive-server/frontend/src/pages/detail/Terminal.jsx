import { useState, useEffect, useCallback, useRef } from 'react'
import { Link, useLocation } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import Button from '../../components/Button.jsx'
import StatusDot from '../../components/StatusDot.jsx'
import { useStatus } from '../../App.jsx'

// Session names have to match the server-side validator
// (regexp: ^[a-zA-Z0-9_-]{1,32}$). We mirror it here so the UI can
// disable the "open" button instead of round-tripping for errors.
const NAME_RE = /^[a-zA-Z0-9_-]{1,32}$/

// ── Page ──────────────────────────────────────────────────────────────

export default function TerminalDetail() {
  const { showToast } = useStatus()
  const location = useLocation()
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)
  const [newName, setNewName] = useState('')
  const [busy, setBusy] = useState(null) // session name being acted on
  const inputRef = useRef(null)

  // promptMode is set by the Caddy redirect on bare /terminal/. When
  // true, the page renders as a session launcher: auto-focuses the
  // name input, and the "open" button navigates in the CURRENT tab
  // (since the user's intent was to land on /terminal/).
  const promptMode = new URLSearchParams(location.search).get('prompt') === '1'

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/services/terminal')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = await res.json()
      setData(json)
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  useEffect(() => {
    if (promptMode && inputRef.current) {
      inputRef.current.focus()
    }
  }, [promptMode])

  // openSession doesn't create anything server-side — it just navigates
  // to /terminal/?arg=<name>. ttyd's --url-arg passes the name to
  // ttyd-tmux.sh, which `tmux new-session -A -s <name>` attaches to an
  // existing session or creates a fresh one. That "attach-or-create"
  // semantic is the whole reason we can have a single button here.
  const openSession = () => {
    const name = newName.trim()
    if (!NAME_RE.test(name)) {
      showToast('name must match [a-zA-Z0-9_-]{1,32}', 'error')
      inputRef.current?.focus()
      return
    }
    const url = `/terminal/?arg=${encodeURIComponent(name)}`
    if (promptMode) {
      // User came from bare /terminal/; keep them in the same tab.
      window.location.href = url
    } else {
      window.open(url, '_blank', 'noopener,noreferrer')
      setNewName('')
      // Give tmux a beat to register the session, then refresh the list.
      setTimeout(load, 500)
    }
  }

  const killSession = async (name) => {
    if (!confirm(`Kill session "${name}"?\n\nAny running processes in this session will be terminated.`)) return
    setBusy(name)
    try {
      const res = await fetch('/api/services/terminal/kill', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      })
      const j = await res.json()
      if (j.success) {
        showToast(`killed ${name}`, 'success')
        load()
      } else {
        showToast(j.error || 'kill failed', 'error')
      }
    } catch (e) {
      showToast(e.message, 'error')
    } finally {
      setBusy(null)
    }
  }

  const attachUrl = (name) => `/terminal/?arg=${encodeURIComponent(name)}`

  if (error && !data) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <h1 className="detail-title">terminal</h1>
        <div className="muted">Failed to load: {error}</div>
      </div>
    )
  }

  if (!data) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <div className="loading" style={{ minHeight: '20vh' }}>
          <StatusDot color="green" pulse />
          <span>loading…</span>
        </div>
      </div>
    )
  }

  const sessions = data.sessions || []
  const nameValid = NAME_RE.test(newName.trim())

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">terminal</h1>
      <div className="detail-sub">tmux-backed web shell · sessions survive disconnects</div>

      <div className="card-grid">
        {/* Service status card */}
        <Card label="status">
          <div className="card-row">
            <span className="k">service</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color={data.running ? 'green' : 'red'} />
              {data.running ? 'running' : 'stopped'}
            </span>
          </div>
          <div className="card-row">
            <span className="k">uptime</span>
            <span className="v">{data.uptime || '—'}</span>
          </div>
          <div className="card-row">
            <span className="k">sessions</span>
            <span className="v">{sessions.length}</span>
          </div>
        </Card>

        {/* Open / new session card */}
        <Card label={promptMode ? 'pick a name to open a session' : 'open a session'}>
          <div className="row" style={{ gap: '0.5rem', alignItems: 'center' }}>
            <input
              ref={inputRef}
              className="input"
              type="text"
              placeholder="name (e.g. work, claude, deploy)"
              value={newName}
              onChange={e => setNewName(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && openSession()}
              style={{ flex: 1 }}
            />
            <Button onClick={openSession} disabled={!nameValid}>
              open
            </Button>
          </div>
          <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.5rem' }}>
            {promptMode
              ? 'a new tmux session is created if the name is free, or you re-attach if it already exists'
              : 'opens /terminal/?arg=<name> in a new tab · re-attaches if the name is taken'}
          </div>
        </Card>

        {/* Sessions card — full width */}
        <Card label={`sessions · ${sessions.length}`} className="full">
          {data.error && (
            <div className="msg-error" style={{ marginBottom: '0.75rem' }}>
              tmux error: {data.error}
            </div>
          )}
          {sessions.length === 0 ? (
            <div className="muted" style={{ fontSize: '0.9rem' }}>
              no sessions yet — pick a name above to create one.
            </div>
          ) : (
            <div className="svc-list">
              {sessions.map(s => (
                <div key={s.name} className="svc-row">
                  <StatusDot
                    color={s.attached ? 'green' : 'yellow'}
                    title={s.attached ? 'attached' : 'detached'}
                  />
                  <div className="svc-main">
                    <span className="svc-name mono">{s.name}</span>
                    <span className="svc-path">
                      {s.windows} {s.windows === 1 ? 'window' : 'windows'}
                      {' · created '}{s.created_rel || '—'}
                      {s.activity_rel && <> · active {s.activity_rel}</>}
                      {s.attached && <> · attached now</>}
                    </span>
                  </div>
                  <div className="svc-actions">
                    <a href={attachUrl(s.name)} target="_blank" rel="noopener noreferrer">
                      attach ↗
                    </a>
                    <button
                      className="link-btn dismiss"
                      disabled={busy === s.name}
                      onClick={() => killSession(s.name)}
                      title={`kill ${s.name}`}
                    >
                      ×
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Card>
      </div>
    </div>
  )
}
