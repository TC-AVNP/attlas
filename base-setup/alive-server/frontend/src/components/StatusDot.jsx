// Small colored status indicator. Color resolves to one of:
//   green  → ok / running
//   yellow → warn / degraded
//   red    → error / down
//   grey   → unknown / not installed
// Pass pulse=true for a loading-state indicator.
export default function StatusDot({ color = 'grey', pulse = false, title }) {
  const cls = `dot dot-${color}${pulse ? ' dot-pulse' : ''}`
  return <span className={cls} title={title} aria-label={title || color} />
}
