// Universe is the placeholder list view for /petboard/. The real
// react-konva canvas comes in task #8. For now, this is the smoke test
// that proves the frontend ↔ backend round-trip works through the
// Caddy + alive-server auth gate. It's intentionally ugly.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Project, Status } from "../api/types";

const PRIORITY_STYLES: Record<string, string> = {
  high: "bg-red-500/20 text-red-300 border-red-500/40",
  medium: "bg-amber-500/20 text-amber-300 border-amber-500/40",
  low: "bg-sky-500/20 text-sky-300 border-sky-500/40",
};

function totalsFor(p: Project): { done: number; total: number } {
  const counts = p.feature_counts ?? {};
  const done = counts.done ?? 0;
  const total = (Object.keys(counts) as Status[]).reduce(
    (sum, k) => sum + (counts[k] ?? 0),
    0,
  );
  return { done, total };
}

function formatHours(minutes: number): string {
  if (minutes < 60) return `${minutes}m`;
  const hours = minutes / 60;
  return hours >= 10 ? `${Math.round(hours)}h` : `${hours.toFixed(1)}h`;
}

export default function Universe() {
  const { data, isLoading, error } = useQuery({
    queryKey: ["projects"],
    queryFn: () => api.listProjects(),
  });

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100 p-8">
      <header className="mb-8">
        <h1 className="text-3xl font-semibold tracking-tight">petboard</h1>
        <p className="mt-1 text-sm text-neutral-400">
          the universe — list view (canvas comes in task #8)
        </p>
      </header>

      {isLoading && <p className="text-neutral-400">loading…</p>}

      {error && (
        <div className="rounded border border-red-500/40 bg-red-500/10 p-4 text-red-300">
          failed to load projects: {(error as Error).message}
        </div>
      )}

      {data && data.projects.length === 0 && (
        <p className="text-neutral-400">no projects yet.</p>
      )}

      {data && data.projects.length > 0 && (
        <ul className="space-y-3">
          {data.projects.map((p) => {
            const { done, total } = totalsFor(p);
            const priorityClass =
              PRIORITY_STYLES[p.priority] ?? PRIORITY_STYLES.medium;
            return (
              <li key={p.id}>
                <Link
                  to={`/p/${p.slug}`}
                  className="block rounded-lg border border-neutral-800 bg-neutral-900/50 p-4 hover:border-neutral-700 hover:bg-neutral-900 transition-colors"
                >
                  <div className="flex items-center gap-4">
                    <span
                      className="h-3 w-3 rounded-full flex-shrink-0"
                      style={{ backgroundColor: p.color }}
                      aria-hidden
                    />
                    <h2 className="text-lg font-medium flex-1">{p.name}</h2>
                    <span
                      className={`px-2 py-0.5 text-xs uppercase tracking-wide rounded border ${priorityClass}`}
                    >
                      {p.priority}
                    </span>
                    <span className="text-sm text-neutral-400 tabular-nums">
                      {done}/{total}
                    </span>
                    <span className="text-sm text-neutral-400 tabular-nums">
                      {formatHours(p.total_minutes)}
                    </span>
                  </div>
                  {p.problem && (
                    <p className="mt-2 text-sm text-neutral-500 line-clamp-2">
                      {p.problem}
                    </p>
                  )}
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </main>
  );
}
