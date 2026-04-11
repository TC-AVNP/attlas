# Attlas Backlog

Unscheduled features. Convert to a locked `details-plan.md` when picked up.

## Costs dashboard

### Why

Today the dashboard shows a single `CLOUD SPEND` card with one MTD number
per provider (GCP total + Anthropic total). That's enough for "is the
bill roughly what I expect?" but not for "which SKUs are driving it?" —
and specifically, we want to watch:

1. **VM on-demand compute cost** — the `e2-standard-4` SKU on
   `simple-zombie`, day-by-day. We need to spot a jump if the VM is
   accidentally left running at a higher tier, or if SUD drops off for
   any reason.
2. **Network egress cost** — the per-GB outbound cost from
   `europe-west1` to the internet. This is the biggest unbounded risk:
   anything on the VM can suddenly burn money if it starts streaming
   data out (openclaw pulling large models, a leaked endpoint, etc).
   Needs its own dedicated line on the dashboard so a spike is
   immediately visible.

Everything else (persistent disk, static IP, minor SKUs) is either
fixed or negligible. Bundle those into "other".

### Where it lives

New detail page in alive-server, mirroring the openclaw detail page
layout:

- Route: `/services/details/costs`
- Link from the main dashboard's existing `CLOUD SPEND` card: add a
  `details →` action in the card footer (same pattern as the openclaw
  card's `open details` link).
- Reuses the same `Card`, `card-headline`, `card-grid` primitives from
  the existing detail pages so no new visual chrome.

### Page layout (3 cards + a chart)

```
back to dashboard ←

costs
cloud spending breakdown, last 30 days

┌──────────────────────────────────────────────────┐
│  VM COMPUTE (e2-standard-4)                      │
│                                                   │
│  $XX.XX  mtd                                      │
│  avg $X.XX/day                                    │
│                                                   │
│  [30-day SVG bar chart, daily buckets]           │
└──────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────┐
│  NETWORK EGRESS                                   │
│                                                   │
│  $X.XX   mtd    ⚠ highlights any day > $1        │
│  X.XX GB out                                      │
│                                                   │
│  [30-day SVG bar chart, daily buckets]           │
└──────────────────────────────────────────────────┘

┌───────────────────┐  ┌──────────────────────────┐
│  OTHER GCP        │  │  ANTHROPIC               │
│                   │  │                          │
│  $X.XX            │  │  $X.XX mtd               │
│  disk, ip, misc   │  │  cost_report API         │
└───────────────────┘  └──────────────────────────┘
```

### Backend

New endpoint: `GET /api/costs/breakdown`

```go
type CostsBreakdown struct {
    VMCompute      CostSeries `json:"vm_compute"`
    NetworkEgress  CostSeries `json:"network_egress"`
    OtherGCP       float64    `json:"other_gcp_mtd_usd"`
    Anthropic      float64    `json:"anthropic_mtd_usd"`
    WindowDays     int        `json:"window_days"` // 30
}

type CostSeries struct {
    MTD       float64   `json:"mtd_usd"`
    AvgDaily  float64   `json:"avg_daily_usd"`
    Daily     []DayCost `json:"daily"`  // last 30, zero-filled
}
```

Data source: the BigQuery billing export set up on Day 3
(`petprojects-488115.billing_export.gcp_billing_export_v1_*`). Two
SQL queries, both filtered to `DATE(usage_start_time) >=
DATE_SUB(CURRENT_DATE(), INTERVAL 30 DAY)`:

1. **VM compute** — filter `sku.description LIKE '%Instance Core%'`
   or `%E2 Instance%` (verify against actual SKU names in the export
   before pinning — this is the biggest unknown). Group by
   `DATE(usage_start_time)`.
2. **Network egress** — filter `sku.description LIKE
   '%Network Internet Egress%'` or equivalent. Group by
   `DATE(usage_start_time)`.
3. **Other GCP** — everything else in the project, MTD only (no daily
   series). Compute as `total_mtd - vm_compute_mtd - network_egress_mtd`.

Cache the whole response for 10 minutes. BigQuery queries cost a few
cents per TB scanned so aggressive caching matters — the daily shards
are small but it adds up if the dashboard is polled.

Anthropic number reuses the existing `fetchAnthropicSpend` MTD path
from `handleCloudSpend`, no new work there.

### Frontend

New page `frontend/src/pages/detail/Costs.jsx`. The 30-day bar chart
follows the same inline-SVG pattern as the openclaw spend card
(7 bars → 30 bars, each a `<rect>` with a `<title>` child for native
tooltips, today bar coloured `--brand`, rest `--accent`).

Egress card highlights the day bar in `--danger` red if the day's
egress cost exceeds $1 — quick visual alarm for a spike.

### Gotchas to handle during implementation

- **Verify SKU descriptions in the actual export before writing the
  WHERE clauses.** GCP SKU names are not stable across time or
  machine families. Run a one-off query first:
  ```sql
  SELECT DISTINCT sku.description
  FROM `petprojects-488115.billing_export.gcp_billing_export_v1_*`
  WHERE DATE(usage_start_time) >= DATE_SUB(CURRENT_DATE(), INTERVAL 7 DAY)
  ORDER BY sku.description
  ```
- **Egress SKUs split by destination region**. There are usually
  separate SKUs for "to same continent", "to worldwide", "to China",
  etc. The card should sum all of them into a single egress number.
- **SUD appears as negative line items** in the export on E2
  machines. Include those in the VM compute sum or the number will
  look inflated compared to the real bill.
- **New days don't land until ~24h after UTC midnight**. Today's
  bar will always be empty or partial. Either label it "today (pending)"
  or exclude it from the window and show the last 30 completed days.
- **Cache invalidation**: 10-min cache is fine for general use but the
  dashboard will lag the actual console view. Good enough until it
  isn't.

### Out of scope (for the first pass)

- Cost alerts / budget webhooks (separate feature, uses the
  `billingbudgets` API that's already enabled in Terraform).
- Forecast / projection ("at this rate the month will cost $X"). Nice
  to have but adds complexity; ship the raw numbers first.
- Per-VM breakdown (we only have one VM, so `simple-zombie` = the
  whole bill). Revisit if a second VM ever lands.
- Anthropic per-model breakdown. The `cost_report` API does expose
  `model` as a dimension but the current code doesn't. Separate follow-up.
- Historical charts beyond 30 days. BigQuery keeps everything forever
  but the dashboard is for "what's happening right now", not an
  archive viewer.
