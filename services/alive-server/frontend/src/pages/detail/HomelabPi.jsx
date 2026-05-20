import { useState, useEffect } from 'react'
import { Link, useParams, useNavigate } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import StatusDot from '../../components/StatusDot.jsx'
import { useStatus } from '../../App.jsx'

function formatDate(iso) {
  if (!iso) return null
  try {
    const d = new Date(iso + (iso.endsWith('Z') ? '' : 'Z'))
    return d.toLocaleString()
  } catch { return iso }
}

function timeSince(iso) {
  if (!iso) return ''
  const d = new Date(iso + (iso.endsWith('Z') ? '' : 'Z'))
  const s = Math.floor((Date.now() - d.getTime()) / 1000)
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

const EVENT_META = {
  provisioned:   { label: 'Provisioned',    icon: '1', color: 'yellow' },
  image_built:            { label: 'Image built',           icon: '2', color: 'yellow' },
  downloaded:             { label: 'Downloaded',             icon: '3', color: 'blue' },
  registered:             { label: 'Registered',             icon: '4', color: 'green' },
  first_metrics:          { label: 'First metrics',          icon: '5', color: 'green' },
  k8s_joined:             { label: 'K8s joined',             icon: '6', color: 'green' },
  provisioned_complete:   { label: 'Provisioning complete',  icon: '7', color: 'green' },
  revoked:                { label: 'Revoked',                icon: '!', color: 'red' },
}

const EXPECTED_EVENTS = ['provisioned', 'image_built', 'downloaded', 'registered', 'first_metrics', 'k8s_joined', 'provisioned_complete']

export default function HomelabPiDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  const { showToast } = useStatus()
  const [timeline, setTimeline] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    const load = async () => {
      try {
        const res = await fetch(`/api/homelab/tokens/${id}/timeline`)
        if (!res.ok) throw new Error('Not found')
        setTimeline(await res.json())
      } catch {
        setTimeline(null)
      } finally {
        setLoading(false)
      }
    }
    load()
    const t = setInterval(load, 15000)
    return () => clearInterval(t)
  }, [id])

  const downloadImage = (url) => {
    const a = document.createElement('a')
    a.href = url
    a.download = url.split('/').pop()
    a.click()
  }

  const revoke = async () => {
    if (!confirm(`Revoke "${timeline.label}"? The machine will be permanently blocked.`)) return
    try {
      const res = await fetch(`/api/homelab/tokens/${id}/revoke`, { method: 'POST' })
      if (!res.ok) throw new Error('Revoke failed')
      showToast(`Revoked: ${timeline.label}`, 'success')
      setTimeline(t => ({ ...t, status: 'revoked' }))
    } catch (e) { showToast(e.message, 'error') }
  }

  const deleteToken = async () => {
    if (!confirm(`Delete "${timeline.label}"? This cannot be undone.`)) return
    try {
      const res = await fetch(`/api/homelab/tokens/${id}`, { method: 'DELETE' })
      if (!res.ok) throw new Error('Delete failed')
      showToast(`Deleted: ${timeline.label}`, 'success')
      navigate('/')
    } catch (e) { showToast(e.message, 'error') }
  }

  if (loading) return <Card label="loading..."><div className="muted">loading...</div></Card>
  if (!timeline) return <Card label="not found"><div className="muted">Pi not found</div><Link to="/">back</Link></Card>

  const eventMap = {}
  for (const e of (timeline.events || [])) {
    eventMap[e.event] = e
  }

  const statusColor = timeline.status === 'revoked' ? 'red' : timeline.status === 'redeemed' ? 'green' : 'yellow'

  return (
    <Card label={`${timeline.label}`} className="full">
      <div className="pi-detail-header">
        <StatusDot color={statusColor} />
        <span className="pi-detail-label">{timeline.label}</span>
        <span className="homelab-node-type-badge">{timeline.node_type}</span>
        <span className="muted">{timeline.status}</span>
      </div>

      <div className="pi-timeline">
        {EXPECTED_EVENTS.map((eventName, i) => {
          const ev = eventMap[eventName]
          const meta = EVENT_META[eventName]
          const happened = !!ev
          const isLast = i === EXPECTED_EVENTS.length - 1

          return (
            <div key={eventName} className={`pi-timeline-event ${happened ? 'done' : 'pending'}`}>
              <div className="pi-timeline-line-container">
                <div className={`pi-timeline-dot ${happened ? meta.color : 'gray'}`}>{meta.icon}</div>
                {!isLast && <div className={`pi-timeline-line ${happened ? '' : 'dashed'}`} />}
              </div>
              <div className="pi-timeline-content">
                <div className="pi-timeline-label">{meta.label}</div>
                {happened ? (
                  <div className="pi-timeline-time">
                    {formatDate(ev.at)} <span className="muted">({timeSince(ev.at)})</span>
                    {ev.detail && <span className="pi-timeline-detail"> — {ev.detail}</span>}
                  </div>
                ) : (
                  <div className="pi-timeline-time muted">waiting...</div>
                )}
              </div>
            </div>
          )
        })}
        {eventMap.revoked && (
          <div className="pi-timeline-event done">
            <div className="pi-timeline-line-container">
              <div className="pi-timeline-dot red">!</div>
            </div>
            <div className="pi-timeline-content">
              <div className="pi-timeline-label">Revoked</div>
              <div className="pi-timeline-time">{formatDate(eventMap.revoked.at)}</div>
            </div>
          </div>
        )}
      </div>

      <div className="pi-detail-actions">
        {timeline.status !== 'revoked' && (
          <button className="link-btn dismiss" onClick={revoke}>revoke</button>
        )}
        <button className="link-btn dismiss" onClick={deleteToken}>delete</button>
      </div>

      <Link to="/" className="link-btn" style={{ marginTop: '1rem', display: 'inline-block' }}>back to dashboard</Link>
    </Card>
  )
}
