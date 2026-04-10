import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import StatusDot from '../../components/StatusDot.jsx'

// ── SVG bar chart ─────────────────────────────────────────────────────
// No lib: 7 rects with <title> children = native browser tooltips.

function BarChart({ days }) {
  if (!days || days.length === 0) {
    return <div className="muted" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>no data yet</div>
  }
  const W = 360, H = 110, PAD = 8, LABEL_H = 18
  const chartH = H - LABEL_H - PAD
  const max = Math.max(0.01, ...days.map(d => d.usd))
  const barW = (W - PAD * 2) / days.length - 6
  const todayISO = new Date().toISOString().slice(0, 10)

  return (
    <svg className="bar-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet">
      {days.map((d, i) => {
        const h = (d.usd / max) * chartH
        const x = PAD + i * ((W - PAD * 2) / days.length) + 3
        const y = chartH - h + PAD / 2
        const isToday = d.date === todayISO
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
              <title>${d.usd.toFixed(2)} on {d.date}</title>
            </rect>
            <text
              className="day-label"
              x={x + barW / 2}
              y={H - 3}
              textAnchor="middle"
            >
              {d.date.slice(8, 10)}
            </text>
          </g>
        )
      })}
    </svg>
  )
}

// ── Page ──────────────────────────────────────────────────────────────

export default function OpenclawDetail() {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const res = await fetch('/api/services/openclaw')
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = await res.json()
        if (!cancelled) setData(json)
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    }
    load()
    const t = setInterval(load, 15000)
    return () => { cancelled = true; clearInterval(t) }
  }, [])

  if (error) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <h1 className="detail-title">openclaw</h1>
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

  const spendThisMonth = data.spend_this_month ?? 0
  const daily = data.spend_daily || []
  const month = new Date().toLocaleString('en-US', { month: 'long' }).toLowerCase()
  const billingOk = !data.billing_error
  const sessions = data.sessions ?? 0
  const tasksRun = data.tasks_run ?? 0
  const activeTasks = data.active_tasks ?? 0

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">openclaw</h1>
      <div className="detail-sub">AI agent daemon</div>

      <div className="card-grid">
        {/* Spend card — full width */}
        <Card label="spend" className="full">
          {billingOk ? (
            <>
              <div className="card-headline">${spendThisMonth.toFixed(2)}</div>
              <div className="card-headline-sub">{month}</div>
              <BarChart days={daily} />
            </>
          ) : (
            <>
              <div className="card-headline" style={{ color: 'var(--muted)' }}>—</div>
              <div className="card-headline-sub">billing data unavailable</div>
            </>
          )}
        </Card>

        {/* Usage card */}
        <Card label="usage · lifetime">
          <div className="card-row">
            <span className="k">sessions</span>
            <span className="v">{sessions}</span>
          </div>
          <div className="card-row">
            <span className="k">tasks run</span>
            <span className="v">{tasksRun}</span>
          </div>
        </Card>

        {/* Status card */}
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
            <span className="k">active</span>
            <span className="v">{activeTasks} {activeTasks === 1 ? 'task' : 'tasks'}</span>
          </div>
        </Card>
      </div>

      <div className="detail-actions">
        <a className="btn btn-primary" href="/openclaw/" target="_blank" rel="noopener noreferrer">
          open openclaw ↗
        </a>
      </div>
    </div>
  )
}
