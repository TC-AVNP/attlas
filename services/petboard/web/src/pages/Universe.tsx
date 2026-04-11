// Universe is the / page. It has two view modes:
//   - canvas: react-konva infinite-zoom universe (the "real" UI)
//   - list:   plain rows for accessibility / debugging
//
// The canvas needs feature data per project, so we fan out to
// /api/projects/:slug for each project after the list loads. react-query
// caches both queries so /p/:slug navigations reuse the data.

import { useEffect, useRef, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Project, ProjectDetail, Status } from "../api/types";
import PriorityPill from "../components/PriorityPill";
import { formatHours } from "../lib/format";
import CanvasUniverse from "../canvas/CanvasUniverse";

// Tiny query for the open-todos count badge in the header.
function useOpenTodoCount(): number {
  const { data } = useQuery({
    queryKey: ["todos", false],
    queryFn: () => api.listTodos(false),
  });
  return data?.todos.length ?? 0;
}

type ViewMode = "canvas" | "list";

function totalsFor(p: Project): { done: number; total: number } {
  const counts = p.feature_counts ?? {};
  const done = counts.done ?? 0;
  const total = (Object.keys(counts) as Status[]).reduce(
    (sum, k) => sum + (counts[k] ?? 0),
    0,
  );
  return { done, total };
}

export default function Universe() {
  const [view, setView] = useState<ViewMode>("canvas");
  const openTodos = useOpenTodoCount();

  const { data, isLoading, error } = useQuery({
    queryKey: ["projects"],
    queryFn: () => api.listProjects(),
  });

  // Fan out to per-project detail queries so the canvas can render orbs.
  // useQueries with `enabled` only fires once we have the project list.
  const projects = data?.projects ?? [];
  const detailQueries = useQueries({
    queries: projects.map((p) => ({
      queryKey: ["project", p.slug],
      queryFn: () => api.getProject(p.slug),
      staleTime: 30_000,
    })),
  });
  const details: Record<string, ProjectDetail> = {};
  detailQueries.forEach((q, i) => {
    if (q.data) details[projects[i].slug] = q.data;
  });

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100 flex flex-col">
      <header className="flex items-center justify-between p-4 border-b border-neutral-900">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">petboard</h1>
          <p className="text-xs text-neutral-500">the universe</p>
        </div>
        <div className="flex items-center gap-2 text-xs">
          <Link
            to="/todos"
            className="px-3 py-1 rounded border border-neutral-700 bg-neutral-900 text-neutral-400 hover:text-neutral-200 flex items-center gap-1.5"
            title="standalone todos that aren't tied to any project"
          >
            todos
            {openTodos > 0 && (
              <span className="px-1 rounded bg-amber-500/20 text-amber-300 text-[10px] tabular-nums">
                {openTodos}
              </span>
            )}
          </Link>
          <button
            type="button"
            onClick={() => setView("canvas")}
            className={`px-3 py-1 rounded border ${
              view === "canvas"
                ? "border-neutral-500 bg-neutral-800 text-neutral-100"
                : "border-neutral-700 bg-neutral-900 text-neutral-400 hover:text-neutral-200"
            }`}
          >
            canvas
          </button>
          <button
            type="button"
            onClick={() => setView("list")}
            className={`px-3 py-1 rounded border ${
              view === "list"
                ? "border-neutral-500 bg-neutral-800 text-neutral-100"
                : "border-neutral-700 bg-neutral-900 text-neutral-400 hover:text-neutral-200"
            }`}
          >
            list
          </button>
        </div>
      </header>

      {isLoading && (
        <div className="p-8 space-y-3 max-w-4xl">
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="h-20 rounded-lg border border-neutral-900 bg-neutral-900/30 animate-pulse"
            />
          ))}
        </div>
      )}
      {error && (
        <div className="m-4 rounded border border-red-500/40 bg-red-500/10 p-4 text-red-300">
          failed to load projects: {(error as Error).message}
        </div>
      )}

      {data && view === "canvas" && (
        <CanvasArea projects={data.projects} details={details} />
      )}

      {data && view === "list" && (
        <ListView projects={data.projects} />
      )}
    </main>
  );
}

// ----- subviews ----------------------------------------------------------

function CanvasArea({
  projects,
  details,
}: {
  projects: Project[];
  details: Record<string, ProjectDetail>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [size, setSize] = useState({ width: 800, height: 600 });

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const update = () => setSize({ width: el.clientWidth, height: el.clientHeight });
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  return (
    <div ref={ref} className="flex-1 min-h-0 overflow-hidden">
      {projects.length === 0 ? (
        <EmptyState />
      ) : (
        <CanvasUniverse
          data={{ projects, details }}
          width={size.width}
          height={size.height}
        />
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex h-full items-center justify-center p-8">
      <div className="max-w-md text-center">
        <div className="mx-auto mb-4 h-12 w-12 rounded-full border border-neutral-800 flex items-center justify-center text-neutral-600">
          ✦
        </div>
        <h2 className="text-lg font-medium text-neutral-200">
          the universe is empty
        </h2>
        <p className="mt-2 text-sm text-neutral-500 leading-relaxed">
          Pet projects show up here as glowing threads on a calendar. Talk to
          Claude Code (the petboard skill is wired to MCP) about a new pet
          project, and it will land on the canvas in real time.
        </p>
      </div>
    </div>
  );
}

function ListView({ projects }: { projects: Project[] }) {
  if (projects.length === 0) {
    return <p className="p-8 text-neutral-400">no projects yet.</p>;
  }
  return (
    <ul className="space-y-3 max-w-4xl p-6">
      {projects.map((p) => {
        const { done, total } = totalsFor(p);
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
                <PriorityPill priority={p.priority} />
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
  );
}
