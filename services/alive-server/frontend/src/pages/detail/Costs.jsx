import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import StatusDot from '../../components/StatusDot.jsx'

// ── Helpers ───────────────────────────────────────────────────────────

function fmtHM(seconds) {
  if (!seconds || seconds <= 0) return '0m'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h === 0) return `${m}m`
  if (m === 0) return `${h}h`
  return `${h}h${m}m`
}

// GCP on-demand hourly rate for e2-standard-4 in europe-west1.
// vCPU: 4 x $0.024291 = $0.097164
// RAM: 16 x $0.003261 = $0.052176
// Total: ~$0.1493/hour
const E2_STD4_HOURLY_USD = 0.1493

// ── Stacked VM uptime bar chart ──────────────────────────────────────

const VM_COLORS = [
  'var(--brand)',
  'var(--accent)',
  '#00d4d4',
  '#ff6bcf',
  '#ffcc00',
  '#8b7bff',
]

function colorFor(idx) {
  return VM_COLORS[idx % VM_COLORS.length]
}

function UptimeChart({ days, series }) {
  const [hover, setHover] = useState(null)

  if (!days?.length || !series?.length) {
    return <div className="muted" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>no data yet</div>
  }

  const W = 720, H = 200, PAD = 8, LABEL_H = 22, YAXIS_W = 40
  const chartH = H - LABEL_H - PAD
  const n = days.length
  const slot = (W - YAXIS_W - PAD) / n
  const barW = Math.max(4, slot - 4)
  const maxSec = 86400

  const yTicks = [0, 6, 12, 18, 24]

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
      <svg className="bar-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" style={{ height: 210 }}>
        {yTicks.map(h => {
          const y = PAD / 2 + chartH - (h / 24) * chartH
          return (
            <g key={h}>
              <text
                x={YAXIS_W - 6}
                y={y + 3}
                textAnchor="end"
                className="day-label"
                style={{ fontSize: '9px' }}
              >
                {h}h
              </text>
              <line
                x1={YAXIS_W}
                y1={y}
                x2={W - PAD}
                y2={y}
                stroke="var(--border)"
                strokeWidth="0.3"
                strokeDasharray={h === 0 ? 'none' : '2,3'}
              />
            </g>
          )
        })}

        {bars.map((bar, i) => {
          const x = YAXIS_W + i * slot + (slot - barW) / 2
          let yCursor = chartH + PAD / 2

          return (
            <g
              key={bar.day}
              onMouseMove={(e) => handleMove(e, bar)}
              onMouseLeave={() => setHover(null)}
            >
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
              <span className="chart-tooltip-val">{fmtHM(seg.secs)}</span>
            </div>
          ))}
          {hover.bar.segments.length > 1 && (
            <div className="chart-tooltip-total">
              total {fmtHM(hover.bar.daySum)}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

function VMLegend({ series }) {
  if (!series?.length) return null
  return (
    <div className="chart-legend">
      {series.map((s, i) => (
        <div key={s.name} className="chart-legend-item">
          <span className="chart-legend-swatch" style={{ background: colorFor(i) }} />
          <span className="chart-legend-name">{s.name}</span>
          <span className="chart-legend-val muted">{fmtHM(s.total_seconds)} this month</span>
        </div>
      ))}
    </div>
  )
}

// ── Page ──────────────────────────────────────────────────────────────

export default function CostsDetail() {
  const [infra, setInfra] = useState(null)
  const [spend, setSpend] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const [infraRes, spendRes] = await Promise.all([
          fetch('/api/services/infrastructure'),
          fetch('/api/cloud-spend'),
        ])
        if (!infraRes.ok) throw new Error(`infrastructure: HTTP ${infraRes.status}`)
        if (!spendRes.ok) throw new Error(`cloud-spend: HTTP ${spendRes.status}`)
        const [infraJson, spendJson] = await Promise.all([infraRes.json(), spendRes.json()])
        if (!cancelled) {
          setInfra(infraJson)
          setSpend(spendJson)
        }
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
        <h1 className="detail-title">costs</h1>
        <div className="muted">Failed to load: {error}</div>
      </div>
    )
  }

  if (!infra || !spend) {
    return (
      <div className="detail-page">
        <Link to="/" className="back-link">← back to dashboard</Link>
        <div className="loading" style={{ minHeight: '20vh' }}>
          <StatusDot color="green" pulse />
          <span>loading...</span>
        </div>
      </div>
    )
  }

  const month = new Date().toLocaleString('en-US', { month: 'long' }).toLowerCase()
  const totalHours = (infra.total_seconds_month || 0) / 3600
  const estimatedCost = totalHours * E2_STD4_HOURLY_USD
  const gcpBilled = spend.gcp_mtd_usd ?? 0
  const hasBillingData = gcpBilled > 0 && !spend.gcp_error

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">costs</h1>
      <div className="detail-sub">infrastructure costs &middot; {month}</div>

      <div className="card-grid">
        {/* VM uptime chart */}
        <Card label={`vm uptime &middot; ${month}`} className="full">
          <div className="card-headline">{fmtHM(infra.total_seconds_month || 0)}</div>
          <div className="card-headline-sub">
            total vm runtime this month &middot; source: gcp cloud monitoring
          </div>
          <UptimeChart
            days={infra.uptime_days || []}
            series={infra.uptime_series || []}
          />
          <VMLegend series={infra.uptime_series || []} />
          {infra.events_error && (
            <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.8rem' }}>
              note: {infra.events_error}
            </div>
          )}
        </Card>

        {/* GCP cost */}
        <Card label={`gcp cost &middot; ${month}`}>
          {hasBillingData ? (
            <>
              <div className="card-headline">${gcpBilled.toFixed(2)}</div>
              <div className="card-headline-sub">
                month-to-date &middot; source: bigquery billing export
              </div>
            </>
          ) : (
            <>
              <div className="card-headline">~${estimatedCost.toFixed(2)}</div>
              <div className="card-headline-sub">
                estimated from {totalHours.toFixed(1)}h runtime &times; ${E2_STD4_HOURLY_USD}/h (e2-standard-4 on-demand)
              </div>
              {spend.gcp_error ? (
                <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.8rem' }}>
                  billing export error: {spend.gcp_error}
                </div>
              ) : (
                <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.8rem' }}>
                  billing export has no data for {month} &mdash; re-enable in gcp console &rarr; billing &rarr; billing export
                </div>
              )}
            </>
          )}
        </Card>
      </div>
    </div>
  )
}
