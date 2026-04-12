import { useState, useEffect, useCallback } from 'react'
import Card from '../../components/Card.jsx'
import Button from '../../components/Button.jsx'
import { useStatus } from '../../App.jsx'

// Splitsies super-admin panel.
//
// Splitsies itself only exposes add-user and remove-user to admins, per
// the feature spec. Admin PROMOTION and DEMOTION deliberately live here
// on the attlas dashboard — whoever has access to this page is the
// super admin for splitsies.
//
// Backend routes (all loopback-trusted against the splitsies backend):
//   GET    /api/services/splitsies              list users
//   POST   /api/services/splitsies/users        add user
//   PATCH  /api/services/splitsies/users/:id    promote/demote
//   DELETE /api/services/splitsies/users/:id    revoke access

export default function SplitsiesDetail() {
  const { showToast } = useStatus()
  const [users, setUsers] = useState(null)
  const [error, setError] = useState(null)
  const [busy, setBusy] = useState(null) // user id being acted on
  const [newEmail, setNewEmail] = useState('')
  const [newAdmin, setNewAdmin] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await fetch('/api/services/splitsies')
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
      const res = await fetch('/api/services/splitsies/users', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, is_admin: newAdmin }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      setNewEmail('')
      setNewAdmin(false)
      await load()
      showToast(`added ${email}${newAdmin ? ' (admin)' : ''}`)
    } catch (e) {
      showToast(`error: ${e.message}`)
    } finally {
      setBusy(null)
    }
  }

  const toggleAdmin = async (user) => {
    setBusy(user.id)
    try {
      const res = await fetch(`/api/services/splitsies/users/${user.id}`, {
        method: 'PATCH',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ is_admin: !user.is_admin }),
      })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      await load()
      showToast(`${user.email} is now ${!user.is_admin ? 'an admin' : 'a regular user'}`)
    } catch (e) {
      showToast(`error: ${e.message}`)
    } finally {
      setBusy(null)
    }
  }

  const removeUser = async (user) => {
    if (!confirm(`Revoke access for ${user.email}? Their history stays intact; they just can't log in anymore.`)) return
    setBusy(user.id)
    try {
      const res = await fetch(`/api/services/splitsies/users/${user.id}`, { method: 'DELETE' })
      if (!res.ok) {
        const text = await res.text().catch(() => '')
        throw new Error(text || `HTTP ${res.status}`)
      }
      await load()
      showToast(`revoked ${user.email}`)
    } catch (e) {
      showToast(`error: ${e.message}`)
    } finally {
      setBusy(null)
    }
  }

  if (error) {
    return (
      <div>
        <h1>Splitsies</h1>
        <Card label="error">
          <p>Could not load users: {error}</p>
        </Card>
      </div>
    )
  }

  if (!users) {
    return (
      <div>
        <h1>Splitsies</h1>
        <Card label="loading"><p>Loading users…</p></Card>
      </div>
    )
  }

  const active = users.filter(u => u.is_active)
  const revoked = users.filter(u => !u.is_active)

  return (
    <div>
      <h1>Splitsies</h1>
      <p className="muted">
        Super-admin panel. Splitsies is at{' '}
        <a href="https://splitsies.attlas.uk/" target="_blank" rel="noopener noreferrer">
          splitsies.attlas.uk
        </a>. Admin promotion/demotion lives here — splitsies&rsquo;s own UI can only
        add and remove regular users.
      </p>

      <Card label={`active users (${active.length})`} className="full">
        <ul className="svc-list">
          {active.map(u => (
            <li key={u.id} className="svc-row">
              <div className="svc-main">
                <span className="svc-name">
                  {u.name || u.email}
                  {u.is_admin && <span className="badge" style={{ marginLeft: 8 }}>admin</span>}
                </span>
                <span className="svc-path muted">{u.email}</span>
              </div>
              <div className="svc-actions">
                <button
                  className="link-btn"
                  disabled={busy === u.id}
                  onClick={() => toggleAdmin(u)}
                  title={u.is_admin ? 'revoke admin' : 'promote to admin'}
                >
                  {u.is_admin ? 'demote' : 'promote'}
                </button>
                <button
                  className="link-btn dismiss"
                  disabled={busy === u.id}
                  onClick={() => removeUser(u)}
                  title="revoke access"
                >
                  ×
                </button>
              </div>
            </li>
          ))}
        </ul>
      </Card>

      <Card label="add user" className="full">
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <input
            type="email"
            value={newEmail}
            onChange={e => setNewEmail(e.target.value)}
            placeholder="user@gmail.com"
            disabled={busy === 'new'}
            style={{ flex: 1, minWidth: 200 }}
          />
          <label style={{ display: 'flex', alignItems: 'center', gap: 4 }}>
            <input
              type="checkbox"
              checked={newAdmin}
              onChange={e => setNewAdmin(e.target.checked)}
              disabled={busy === 'new'}
            />
            admin
          </label>
          <Button onClick={addUser} disabled={!newEmail.trim() || busy === 'new'}>
            {busy === 'new' ? 'adding…' : 'add'}
          </Button>
        </div>
      </Card>

      {revoked.length > 0 && (
        <Card label={`revoked (${revoked.length})`} className="full">
          <ul className="svc-list">
            {revoked.map(u => (
              <li key={u.id} className="svc-row" style={{ opacity: 0.6 }}>
                <div className="svc-main">
                  <span className="svc-name">{u.name || u.email}</span>
                  <span className="svc-path muted">{u.email} · revoked (history preserved)</span>
                </div>
              </li>
            ))}
          </ul>
        </Card>
      )}
    </div>
  )
}
