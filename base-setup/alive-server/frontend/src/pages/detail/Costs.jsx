import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import Card from '../../components/Card.jsx'
import StatusDot from '../../components/StatusDot.jsx'

// ── 30-day bar chart ──────────────────────────────────────────────────
// Same shape as the openclaw spend chart (inline SVG, native <title>
// tooltips) but with 30 bars instead of 7 and an optional spike
// threshold that paints over-budget days in --danger red.

function BarChart30({ days, spikeThreshold = null }) {
  if (!days || days.length === 0) {
    return <div className="muted" style={{ marginTop: '1rem', fontSize: '0.85rem' }}>no data yet</div>
  }
  const W = 720, H = 160, PAD = 8, LABEL_H = 22
  const chartH = H - LABEL_H - PAD
  const n = days.length
  const slot = (W - PAD * 2) / n
  const barW = Math.max(4, slot - 4)
  // Y-axis max is the largest observed value with a floor so a
  // zero-filled chart still renders a flat baseline rather than
  // dividing by zero.
  const max = Math.max(0.01, ...days.map(d => d.usd))
  const todayISO = new Date().toISOString().slice(0, 10)

  return (
    <svg className="bar-chart" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid meet" style={{ height: 170 }}>
      <line
        x1={PAD}
        y1={chartH + PAD / 2}
        x2={W - PAD}
        y2={chartH + PAD / 2}
        stroke="var(--border)"
        strokeWidth="0.5"
      />
      {days.map((d, i) => {
        const ratio = d.usd / max
        const h = ratio * chartH
        const x = PAD + i * slot + (slot - barW) / 2
        const y = chartH - h + PAD / 2
        const isToday = d.date === todayISO
        const overSpike = spikeThreshold !== null && d.usd >= spikeThreshold
        let cls = 'bar'
        if (overSpike)   cls = 'bar bar-danger'
        else if (isToday) cls = 'bar bar-today'
        return (
          <g key={d.date}>
            <rect
              className={cls}
              x={x}
              y={y}
              width={barW}
              height={Math.max(1, h)}
              rx="1"
            >
              <title>${d.usd.toFixed(4)} on {d.date}</title>
            </rect>
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

export default function CostsDetail() {
  const [data, setData] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const res = await fetch('/api/costs/breakdown')
        if (!res.ok) throw new Error(`HTTP ${res.status}`)
        const json = await res.json()
        if (!cancelled) setData(json)
      } catch (e) {
        if (!cancelled) setError(e.message)
      }
    }
    load()
    // 60s — endpoint is cached server-side for 10 min anyway, this
    // just keeps a stale tab fresh-ish if left open.
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

  const vm = data.vm_compute || { total_30d_usd: 0, avg_daily_usd: 0, daily: [] }
  const eg = data.network_egress || { total_30d_usd: 0, avg_daily_usd: 0, daily: [] }
  const otherGCP = data.other_gcp_30d_usd ?? 0
  const anthropic = data.anthropic_30d_usd ?? 0
  const gcpOk = !data.gcp_error
  const anthOk = !data.anthropic_error
  const windowDays = data.window_days || 30

  return (
    <div className="detail-page">
      <Link to="/" className="back-link">← back to dashboard</Link>
      <h1 className="detail-title">costs</h1>
      <div className="detail-sub">cloud spending breakdown · last {windowDays} completed days</div>

      <div className="card-grid">
        {/* VM compute — full width. The headline metric we actually
            care about watching day-to-day: is simple-zombie costing
            what we expect? */}
        <Card label={`vm compute · last ${windowDays} days`} className="full">
          {gcpOk ? (
            <>
              <div className="card-headline">${vm.total_30d_usd.toFixed(2)}</div>
              <div className="card-headline-sub">
                avg ${vm.avg_daily_usd.toFixed(2)}/day · e2-standard-4 · source: bigquery billing export
              </div>
              <BarChart30 days={vm.daily} />
            </>
          ) : (
            <>
              <div className="card-headline" style={{ color: 'var(--muted)' }}>—</div>
              <div className="card-headline-sub">
                gcp billing data unavailable · {data.gcp_error}
              </div>
            </>
          )}
        </Card>

        {/* Network egress — full width. Second headline: egress is
            the one line item that can spike without warning if
            anything on the VM starts streaming outbound. Days above
            the spike threshold render red. */}
        <Card label={`network egress · last ${windowDays} days`} className="full">
          {gcpOk ? (
            <>
              <div className="card-headline">${eg.total_30d_usd.toFixed(2)}</div>
              <div className="card-headline-sub">
                avg ${eg.avg_daily_usd.toFixed(2)}/day · red bars flag days above $1
              </div>
              <BarChart30 days={eg.daily} spikeThreshold={1.0} />
            </>
          ) : (
            <>
              <div className="card-headline" style={{ color: 'var(--muted)' }}>—</div>
              <div className="card-headline-sub">
                gcp billing data unavailable · {data.gcp_error}
              </div>
            </>
          )}
        </Card>

        {/* Other GCP — disk, IP, misc minor SKUs rolled into one. */}
        <Card label={`other gcp · last ${windowDays} days`}>
          <div className="card-headline">${otherGCP.toFixed(2)}</div>
          <div className="card-headline-sub">
            disk, ip, minor skus
          </div>
          {!gcpOk && (
            <div className="muted" style={{ fontSize: '0.8rem', marginTop: '0.8rem' }}>
              note: {data.gcp_error}
            </div>
          )}
        </Card>

        {/* Anthropic — pulled from the same cost_report API as the
            main dashboard's cloud-spend card, but on a 30-day window
            for parity with the GCP cards. */}
        <Card label={`anthropic · last ${windowDays} days`}>
          {anthOk ? (
            <>
              <div className="card-headline">${anthropic.toFixed(2)}</div>
              <div className="card-headline-sub">
                source: cost_report api
              </div>
            </>
          ) : (
            <>
              <div className="card-headline" style={{ color: 'var(--muted)' }}>—</div>
              <div className="card-headline-sub">
                data unavailable · {data.anthropic_error}
              </div>
            </>
          )}
        </Card>
      </div>
    </div>
  )
}
