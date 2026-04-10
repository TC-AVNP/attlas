import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import StatusDot from '../../components/StatusDot.jsx'

// ── Helpers ───────────────────────────────────────────────────────────

function hoursMinutes(seconds) {
  if (!seconds || seconds <= 0) return '0h'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h === 0) return `${m}m`
  if (m === 0) return `${h}h`
  return `${h}h ${m}m`
}

function totalHoursLabel(totalSeconds) {
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const mins = Math.floor((totalSeconds % 3600) / 60)
  if (days > 0) return `${days}d ${hours}h ${mins}m`
  return `${hours}h ${mins}m`
}

function formatISO(iso) {
  if (!iso) return '—'
  try {
    const d = new Date(iso)
    return d.toISOString().replace('T', ' ').slice(0, 16) + ' UTC'
  } catch {
    return iso
  }
}

// ── Daily uptime bar chart ────────────────────────────────────────────
// One bar per day of the current month. Height = hours / 24. Today's
// bar is colored --brand; past bars are --accent. Native <title> for
// hover tooltips.

function UptimeChart({ days }) {
  if (!days || days.length === 0) {
    return <div className="muted" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>no data yet</div>
  }
  const W = 720, H = 160, PAD = 8, LABEL_H = 22
  const chartH = H - LABEL_H - PAD
  const n = days.length
  const slot = (W - PAD * 2) / n
  const barW = Math.max(4, slot - 4)
  const todayISO = new Date().toISOString().slice(0, 10)
  const maxSec = 86400

  return (
    <svg className="bar-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" style={{ height: 170 }}>
      {/* baseline rule */}
      <line
        x1={PAD}
        y1={chartH + PAD / 2}
        x2={W - PAD}
        y2={chartH + PAD / 2}
        stroke="var(--border)"
        strokeWidth="0.5"
      />
      {days.map((d, i) => {
        const ratio = Math.min(1, d.seconds / maxSec)
        const h = ratio * chartH
        const x = PAD + i * slot + (slot - barW) / 2
        const y = chartH - h + PAD / 2
        const isToday = d.date === todayISO
        const hours = (d.seconds / 3600).toFixed(1)
        return (
          <g key={d.date}>
            <rect
              className={isToday ? 'bar bar-today' : 'bar'}
              x={x}
              y={y}
              width={barW}
              height={Math.max(1, h)}
              rx="1"
            >
              <title>{d.date}: {hoursMinutes(d.seconds)} of uptime</title>
            </rect>
            {/* label every 5 days to stay readable */}
            {(i === 0 || (i + 1) % 5 === 0 || i === days.length - 1) && (
              <text
                className="day-label"
                x={x + barW / 2}
                y={H - 6}
                textAnchor="middle"
              >
                {d.date.slice(8, 10)}
              </text>
            )}
          </g>
        )
      })}
    </svg>
  )
}

// ── Page ──────────────────────────────────────────────────────────────

export default function InfrastructureDetail() {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const res = await fetch('/api/services/infrastructure')
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = await res.json()
        if (!cancelled) setData(json)
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    }
    load()
    const t = setInterval(load, 60000)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

  if (error) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <h1 className="detail-title">infrastructure</h1>
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

  const month = new Date().toLocaleString('en-US', { month: 'long' }).toLowerCase()

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">infrastructure</h1>
      <div className="detail-sub">gcp compute engine vm</div>

      <div className="card-grid">
        {/* Uptime chart — full width */}
        <Card label={`daily uptime · ${month}`} className="full">
          <div className="card-headline">{totalHoursLabel(data.total_seconds_month || 0)}</div>
          <div className="card-headline-sub">
            total uptime this month · source: gcp cloud logging audit events
          </div>
          <UptimeChart days={data.daily_uptime || []} />
          {data.events_error && (
            <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.8rem' }}>
              note: {data.events_error}
            </div>
          )}
        </Card>

        {/* VM identity */}
        <Card label="vm">
          <div className="card-row">
            <span className="k">name</span>
            <span className="v">{data.name}</span>
          </div>
          <div className="card-row">
            <span className="k">machine</span>
            <span className="v">{data.machine_type}</span>
          </div>
          <div className="card-row">
            <span className="k">zone</span>
            <span className="v">{data.zone}</span>
          </div>
          <div className="card-row">
            <span className="k">region</span>
            <span className="v">{data.region}</span>
          </div>
          <div className="card-row">
            <span className="k">created</span>
            <span className="v">{data.creation_timestamp ? data.creation_timestamp.slice(0, 10) : '—'}</span>
          </div>
        </Card>

        {/* Network */}
        <Card label="network">
          <div className="card-row">
            <span className="k">external ip</span>
            <span className="v">{data.external_ip}</span>
          </div>
          <div className="card-row">
            <span className="k">internal ip</span>
            <span className="v">{data.internal_ip}</span>
          </div>
          <div className="card-row">
            <span className="k">domain</span>
            <span className="v">
              <a href={`https://${data.domain}/`}>{data.domain}</a>
            </span>
          </div>
        </Card>

        {/* Current session */}
        <Card label="current session" className="full">
          <div className="card-row">
            <span className="k">status</span>
            <span className="v" style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
              <StatusDot color="green" />
              running
            </span>
          </div>
          <div className="card-row">
            <span className="k">uptime</span>
            <span className="v">{data.uptime_now || '—'}</span>
          </div>
          <div className="card-row">
            <span className="k">booted</span>
            <span className="v">{formatISO(data.os_boot_time)}</span>
          </div>
        </Card>
      </div>
    </div>
  )
}
