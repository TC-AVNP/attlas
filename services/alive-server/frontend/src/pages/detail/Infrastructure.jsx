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

// ── Stacked daily uptime chart ────────────────────────────────────────
//
// One bar per day in the window. Each bar is a VERTICAL STACK of
// per-VM segments — if multiple VMs were running on the same day,
// each gets its own segment inside the bar so the total height
// matches the cumulative VM-hours for that day (capped at 24h of
// visual height = 86400s). A custom React tooltip follows the cursor
// and shows per-VM breakdown on hover; no more native <title> with
// a 2-second delay.
//
// The data source is Cloud Monitoring's compute.googleapis.com/
// instance/uptime metric, so what you see here is Google's ground
// truth — guest-OS shutdowns, host maintenance, preemptions, and
// crashes all show up correctly as "not running" instead of the
// silent 24h phantom uptime that the old audit-log replay produced.

// Palette for stacked VM segments. First entry goes to the series
// with the most total uptime (sorted server-side). Picked to stay
// distinct on both dark and light themes.
const VM_COLORS = [
  'var(--brand)',   // e.g. #ff8700
  'var(--accent)',  // e.g. #00ff00
  '#00d4d4',        // cyan
  '#ff6bcf',        // pink
  '#ffcc00',        // yellow
  '#8b7bff',        // purple
]

function colorFor(idx) {
  return VM_COLORS[idx % VM_COLORS.length]
}

function StackedUptimeChart({ days, series }) {
  const [hover, setHover] = useState(null)

  if (!days?.length || !series?.length) {
    return <div className="muted" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>no data yet</div>
  }

  const W = 720, H = 160, PAD = 8, LABEL_H = 22
  const chartH = H - LABEL_H - PAD
  const n = days.length
  const slot = (W - PAD * 2) / n
  const barW = Math.max(4, slot - 4)
  const maxSec = 86400

  // Pre-compute all bar segments so the JSX stays flat and the
  // tooltip code can reference the same data.
  const bars = days.map((day, i) => {
    const segments = []
    let daySum = 0
    series.forEach((s, sIdx) => {
      const secs = (s.daily && s.daily[i]) || 0
      if (secs <= 0) return
      daySum += secs
      segments.push({ name: s.name, secs, colorIdx: sIdx })
    })
    return { day, segments, daySum }
  })

  const handleMove = (e, bar) => {
    const svg = e.currentTarget.ownerSVGElement || e.currentTarget
    const rect = svg.getBoundingClientRect()
    setHover({
      bar,
      x: e.clientX - rect.left,
      y: e.clientY - rect.top,
    })
  }

  return (
    <div className="chart-wrap">
      <svg className="bar-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" style={{ height: 170 }}>
        <line
          x1={PAD}
          y1={chartH + PAD / 2}
          x2={W - PAD}
          y2={chartH + PAD / 2}
          stroke="var(--border)"
          strokeWidth="0.5"
        />
        {bars.map((bar, i) => {
          const x = PAD + i * slot + (slot - barW) / 2
          let yCursor = chartH + PAD / 2 // stack from the baseline upward

          return (
            <g
              key={bar.day}
              onMouseMove={(e) => handleMove(e, bar)}
              onMouseLeave={() => setHover(null)}
            >
              {/* invisible full-height hit rect so hover still works
                  on days with zero uptime (no segments) */}
              <rect
                x={x - 2}
                y={PAD / 2}
                width={barW + 4}
                height={chartH}
                fill="transparent"
                pointerEvents="all"
              />
              {bar.segments.map((seg, segIdx) => {
                const ratio = Math.min(1, seg.secs / maxSec)
                const h = ratio * chartH
                yCursor -= h
                return (
                  <rect
                    key={segIdx}
                    x={x}
                    y={yCursor}
                    width={barW}
                    height={Math.max(1, h)}
                    rx="1"
                    fill={colorFor(seg.colorIdx)}
                    pointerEvents="none"
                  />
                )
              })}
              {(i === 0 || (i + 1) % 5 === 0 || i === bars.length - 1) && (
                <text
                  className="day-label"
                  x={x + barW / 2}
                  y={H - 6}
                  textAnchor="middle"
                >
                  {bar.day.slice(8, 10)}
                </text>
              )}
            </g>
          )
        })}
      </svg>
      {hover && (
        <div
          className="chart-tooltip"
          style={{
            left: Math.min(hover.x + 12, 620),
            top: Math.max(hover.y - 12, 0),
          }}
        >
          <div className="chart-tooltip-date">{hover.bar.day}</div>
          {hover.bar.segments.length === 0 && (
            <div className="chart-tooltip-row muted">no uptime</div>
          )}
          {hover.bar.segments.map((seg, i) => (
            <div key={i} className="chart-tooltip-row">
              <span className="chart-tooltip-swatch" style={{ background: colorFor(seg.colorIdx) }} />
              <span className="chart-tooltip-name">{seg.name}</span>
              <span className="chart-tooltip-val">{hoursMinutes(seg.secs)}</span>
            </div>
          ))}
          {hover.bar.segments.length > 1 && (
            <div className="chart-tooltip-total">
              total {hoursMinutes(hover.bar.daySum)}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function UptimeLegend({ series }) {
  if (!series?.length) return null
  return (
    <div className="chart-legend">
      {series.map((s, i) => (
        <div key={s.name} className="chart-legend-item">
          <span className="chart-legend-swatch" style={{ background: colorFor(i) }} />
          <span className="chart-legend-name">{s.name}</span>
          <span className="chart-legend-val muted">{hoursMinutes(s.total_seconds)}</span>
        </div>
      ))}
    </div>
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
        {/* Uptime chart — full width, stacked per VM */}
        <Card label={`daily uptime · ${month}`} className="full">
          <div className="card-headline">{totalHoursLabel(data.total_seconds_month || 0)}</div>
          <div className="card-headline-sub">
            total uptime this month · source: gcp cloud monitoring (instance/uptime)
          </div>
          <StackedUptimeChart
            days={data.uptime_days || []}
            series={data.uptime_series || []}
          />
          <UptimeLegend series={data.uptime_series || []} />
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
