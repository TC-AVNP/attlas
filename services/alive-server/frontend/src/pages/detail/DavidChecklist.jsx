import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import Button from '../../components/Button.jsx'
import StatusDot from '../../components/StatusDot.jsx'
import { useStatus } from '../../App.jsx'

// David's Checklist — user management detail page.
//
// Backend routes (loopback-trusted against the david backend):
//   GET    /api/services/david-s-checklist              list users
//   POST   /api/services/david-s-checklist/users        add user
//   PATCH  /api/services/david-s-checklist/users/:email promote/demote
//   DELETE /api/services/david-s-checklist/users/:email revoke access

export default function DavidChecklistDetail() {
  const { showToast } = useStatus()
  const [users, setUsers] = useState(null)
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(null)
  const [newEmail, setNewEmail] = useState('')
  const [newRole, setNewRole] = useState('user')
  const [confirmRemove, setConfirmRemove] = useState(null)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/services/david-s-checklist')
      if (!res.ok) throw new Error(`HTTP ${res.status}`)
      const json = await res.json()
      setUsers(json)
      setError(null)
    } catch (e) {
      setError(e.message)
    }
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 10000)
    return () => clearInterval(t)
  }, [load])

  const addUser = async () => {
    const email = newEmail.trim().toLowerCase()
    if (!email) return
    setBusy('new')
    try {
      const res = await fetch('/api/services/david-s-checklist/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, is_admin: newRole === 'admin' }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      setNewEmail('')
      setNewRole('user')
      await load()
      showToast(`added ${email} as ${newRole}`)
    } catch (e) {
      showToast(`error: ${e.message}`, 'error')
    } finally {
      setBusy(null)
    }
  }

  const changeRole = async (user, makeAdmin) => {
    setBusy(user.email)
    try {
      const res = await fetch(`/api/services/david-s-checklist/users/${encodeURIComponent(user.email)}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ is_admin: makeAdmin }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      await load()
      showToast(`${user.email} → ${makeAdmin ? 'admin' : 'user'}`)
    } catch (e) {
      showToast(`error: ${e.message}`, 'error')
    } finally {
      setBusy(null)
    }
  }

  const removeUser = async (user) => {
    setBusy(user.email)
    try {
      const res = await fetch(`/api/services/david-s-checklist/users/${encodeURIComponent(user.email)}`, {
        method: 'DELETE',
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      await load()
      showToast(`removed ${user.email}`)
    } catch (e) {
      showToast(`error: ${e.message}`, 'error')
    } finally {
      setBusy(null)
      setConfirmRemove(null)
    }
  }

  // ── Error state ──────────────────────────────────────────────────────
  if (error && !users) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <h1 className="detail-title">david's checklist</h1>
        <div className="muted">Failed to load: {error}</div>
      </div>
    )
  }

  // ── Loading state ───────────────────────────────────────────────────
  if (!users) {
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

  const admins = users.filter(u => u.is_admin)
  const regular = users.filter(u => !u.is_admin)

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">david's checklist</h1>
      <div className="detail-sub">
        user management ·{' '}
        <a href="https://david.attlas.uk/" target="_blank" rel="noopener noreferrer">
          david.attlas.uk
        </a>
      </div>

      <div className="card-grid">
        {/* Summary card */}
        <Card label="overview">
          <div className="card-row">
            <span className="k">total users</span>
            <span className="v">{users.length}</span>
          </div>
          <div className="card-row">
            <span className="k">admins</span>
            <span className="v">{admins.length}</span>
          </div>
          <div className="card-row">
            <span className="k">online now</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color={users.some(u => u.logged_in) ? 'green' : 'grey'} />
              {users.filter(u => u.logged_in).length}
            </span>
          </div>
        </Card>

        {/* Add user card */}
        <Card label="add user">
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.75rem' }}>
            <input
              className="input"
              type="email"
              placeholder="email address"
              value={newEmail}
              onChange={e => setNewEmail(e.target.value)}
              onKeyDown={e => e.key === 'Enter' && newEmail.trim() && addUser()}
              disabled={busy === 'new'}
            />
            <div style={{ display: 'flex', gap: '0.75rem', alignItems: 'center' }}>
              <select
                className="input"
                value={newRole}
                onChange={e => setNewRole(e.target.value)}
                disabled={busy === 'new'}
                style={{ flex: '0 0 auto', width: 'auto', minWidth: '7rem' }}
              >
                <option value="user">user</option>
                <option value="admin">admin</option>
              </select>
              <Button onClick={addUser} disabled={!newEmail.trim() || busy === 'new'}>
                {busy === 'new' ? 'adding…' : 'add user'}
              </Button>
            </div>
          </div>
        </Card>

        {/* Users table — full width */}
        <Card label={`all users · ${users.length}`} className="full">
          {users.length === 0 ? (
            <div className="muted">no users yet — add one above.</div>
          ) : (
            <div className="svc-list">
              {/* Header row */}
              <div style={{
                display: 'grid',
                gridTemplateColumns: '1rem 1fr 6rem 7rem 5rem',
                gap: '0.9rem',
                padding: '0 0 0.5rem',
                borderBottom: '1px solid var(--border)',
                fontSize: '0.72rem',
                fontFamily: 'var(--font-mono)',
                color: 'var(--muted)',
                textTransform: 'uppercase',
                letterSpacing: '0.1em',
              }}>
                <span></span>
                <span>email</span>
                <span>role</span>
                <span>assignments</span>
                <span></span>
              </div>

              {/* User rows */}
              {users.map(u => (
                <div key={u.email} style={{
                  display: 'grid',
                  gridTemplateColumns: '1rem 1fr 6rem 7rem 5rem',
                  gap: '0.9rem',
                  alignItems: 'center',
                  padding: '0.65rem 0',
                  borderBottom: '1px solid var(--border)',
                }}>
                  {/* Online dot */}
                  <StatusDot
                    color={u.logged_in ? 'green' : 'grey'}
                    title={u.logged_in ? 'online' : 'offline'}
                  />

                  {/* Email */}
                  <span style={{ fontSize: '0.92rem', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                    {u.email}
                  </span>

                  {/* Role dropdown */}
                  <select
                    className="input"
                    value={u.is_admin ? 'admin' : 'user'}
                    onChange={e => changeRole(u, e.target.value === 'admin')}
                    disabled={busy === u.email}
                    style={{ fontSize: '0.82rem', padding: '0.25rem 0.4rem' }}
                  >
                    <option value="user">user</option>
                    <option value="admin">admin</option>
                  </select>

                  {/* Task count */}
                  <span className="mono muted" style={{ fontSize: '0.85rem' }}>
                    {u.tasks > 0 ? `${u.tasks} item${u.tasks !== 1 ? 's' : ''}` : '—'}
                  </span>

                  {/* Remove action */}
                  <div style={{ textAlign: 'right' }}>
                    {confirmRemove === u.email ? (
                      <span style={{ display: 'flex', gap: '0.5rem', justifyContent: 'flex-end' }}>
                        <button
                          className="link-btn"
                          style={{ color: 'var(--danger)', fontWeight: 'bold', fontSize: '0.82rem' }}
                          disabled={busy === u.email}
                          onClick={() => removeUser(u)}
                        >
                          yes
                        </button>
                        <button
                          className="link-btn"
                          style={{ fontSize: '0.82rem' }}
                          onClick={() => setConfirmRemove(null)}
                        >
                          no
                        </button>
                      </span>
                    ) : (
                      <Button
                        variant="danger"
                        onClick={() => setConfirmRemove(u.email)}
                        disabled={busy === u.email}
                        style={{ fontSize: '0.75rem', padding: '0.3rem 0.65rem' }}
                      >
                        remove
                      </Button>
                    )}
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
