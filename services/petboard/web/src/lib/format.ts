// Small formatting helpers shared across pages.

export function formatHours(minutes: number): string {
  if (!minutes) return "0h";
  if (minutes < 60) return `${minutes}m`;
  const hours = minutes / 60;
  return hours >= 10 ? `${Math.round(hours)}h` : `${hours.toFixed(1)}h`;
}

export function formatDate(unixSeconds: number): string {
  const d = new Date(unixSeconds * 1000);
  return d.toLocaleDateString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

export function formatRelative(unixSeconds: number, nowSeconds = Date.now() / 1000): string {
  const diff = Math.abs(nowSeconds - unixSeconds);
  if (diff < 60) return "just now";
  if (diff < 3600) return `${Math.round(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.round(diff / 3600)}h ago`;
  if (diff < 86400 * 30) return `${Math.round(diff / 86400)}d ago`;
  if (diff < 86400 * 365) return `${Math.round(diff / (86400 * 30))}mo ago`;
  return `${Math.round(diff / (86400 * 365))}y ago`;
}
