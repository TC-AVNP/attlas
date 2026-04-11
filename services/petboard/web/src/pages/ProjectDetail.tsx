// ProjectDetail is the per-project page. Task #6 ships only the
// minimum to prove the route + data fetch work — full layout (editable
// problem block, four-column board, effort sparkline) is task #7.

import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Feature, Status } from "../api/types";

const STATUS_LABEL: Record<Status, string> = {
  backlog: "backlog",
  in_progress: "in progress",
  done: "done",
  dropped: "dropped",
};

const STATUS_ORDER: Status[] = ["backlog", "in_progress", "done", "dropped"];

function groupByStatus(features: Feature[]): Record<Status, Feature[]> {
  const groups: Record<Status, Feature[]> = {
    backlog: [],
    in_progress: [],
    done: [],
    dropped: [],
  };
  for (const f of features) groups[f.status].push(f);
  return groups;
}

export default function ProjectDetail() {
  const { slug = "" } = useParams();
  const { data, isLoading, error } = useQuery({
    queryKey: ["project", slug],
    queryFn: () => api.getProject(slug),
    enabled: slug.length > 0,
  });

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100 p-8">
      <Link
        to="/"
        className="text-sm text-neutral-400 hover:text-neutral-200"
      >
        ← back to universe
      </Link>

      {isLoading && <p className="mt-4 text-neutral-400">loading…</p>}

      {error && (
        <div className="mt-4 rounded border border-red-500/40 bg-red-500/10 p-4 text-red-300">
          failed to load project: {(error as Error).message}
        </div>
      )}

      {data && (
        <>
          <header className="mt-4 flex items-center gap-3">
            <span
              className="h-4 w-4 rounded-full"
              style={{ backgroundColor: data.color }}
              aria-hidden
            />
            <h1 className="text-3xl font-semibold tracking-tight">
              {data.name}
            </h1>
            <span className="px-2 py-0.5 text-xs uppercase tracking-wide rounded border border-neutral-700 text-neutral-400">
              {data.priority}
            </span>
          </header>

          <blockquote className="mt-6 max-w-3xl border-l-2 border-neutral-700 pl-4 text-neutral-300 italic">
            {data.problem}
          </blockquote>

          <section className="mt-8">
            <h2 className="text-sm uppercase tracking-wide text-neutral-500 mb-3">
              features ({data.features.length})
            </h2>
            <div className="space-y-4">
              {STATUS_ORDER.map((status) => {
                const groups = groupByStatus(data.features);
                const items = groups[status];
                if (items.length === 0) return null;
                return (
                  <div key={status}>
                    <h3 className="text-xs uppercase tracking-wider text-neutral-500 mb-1">
                      {STATUS_LABEL[status]} ({items.length})
                    </h3>
                    <ul className="space-y-1">
                      {items.map((f) => (
                        <li
                          key={f.id}
                          className="text-sm text-neutral-200 pl-2 border-l border-neutral-800"
                        >
                          {f.title}
                        </li>
                      ))}
                    </ul>
                  </div>
                );
              })}
            </div>
          </section>

          <section className="mt-8">
            <h2 className="text-sm uppercase tracking-wide text-neutral-500 mb-3">
              effort log ({data.effort.length})
            </h2>
            <ul className="text-sm text-neutral-300 space-y-1">
              {data.effort.map((e) => (
                <li key={e.id}>
                  {e.minutes}m {e.note ? `— ${e.note}` : ""}
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
    </main>
  );
}
