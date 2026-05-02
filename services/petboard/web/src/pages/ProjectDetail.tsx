// ProjectDetail is the per-project page at /petboard/p/:slug.
//
// Layout:
//   - Header: color dot, name (editable), priority pill (clickable to cycle)
//   - Problem block: pull-quote styled, click to edit, blur to save
//   - Four-column board: Backlog / In Progress / Done / Dropped
//     - Each card has a status-cycling button and a delete button
//     - Click the status chip to advance through the workflow
//   - Add-feature input at the top of Backlog
//   - Effort sparkline + log-effort form
//
// All mutations go through react-query mutations that invalidate the
// ["project", slug] and ["projects"] caches on success so the universe
// view stays in sync.

import { useState, useRef, useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Feature, Priority, Status, ProjectDetail as ProjectDetailType } from "../api/types";
import PriorityPill from "../components/PriorityPill";
import { formatDate, formatHours, formatRelative } from "../lib/format";

const STATUS_LABEL: Record<Status, string> = {
  backlog: "backlog",
  in_progress: "in progress",
  done: "done",
  dropped: "dropped",
};

const STATUS_ORDER: Status[] = ["backlog", "in_progress", "done", "dropped"];

const STATUS_NEXT: Record<Status, Status> = {
  backlog: "in_progress",
  in_progress: "done",
  done: "backlog",
  dropped: "backlog",
};

const STATUS_BORDER: Record<Status, string> = {
  backlog: "border-neutral-700",
  in_progress: "border-amber-500/50",
  done: "border-emerald-500/50",
  dropped: "border-neutral-800",
};

const STATUS_DOT: Record<Status, string> = {
  backlog: "bg-neutral-500",
  in_progress: "bg-amber-400 animate-pulse",
  done: "bg-emerald-400",
  dropped: "bg-neutral-700",
};

const PRIORITY_NEXT: Record<Priority, Priority> = {
  high: "medium",
  medium: "low",
  low: "high",
};

function generateHandoffMarkdown(
  project: ProjectDetailType,
  selectedFeatureIds: Set<number>,
): string {
  const lines: string[] = [];
  lines.push(`# ${project.name}\n`);
  lines.push(`## Problem\n`);
  lines.push(`${project.problem}\n`);
  if (project.description) {
    lines.push(`## Description\n`);
    lines.push(`${project.description}\n`);
  }

  const features = (project.features ?? []).filter((f) =>
    selectedFeatureIds.has(f.id),
  );

  if (features.length > 0) {
    lines.push(`## Features\n`);
    for (const status of STATUS_ORDER) {
      const group = features.filter((f) => f.status === status);
      if (group.length === 0) continue;
      lines.push(`### ${STATUS_LABEL[status]} (${group.length})\n`);
      for (const f of group) {
        lines.push(`- **${f.title}**`);
        if (f.description) {
          lines.push(`  ${f.description}`);
        }
        lines.push("");
      }
    }
  }

  return lines.join("\n");
}

function downloadMarkdown(content: string, filename: string) {
  const blob = new Blob([content], { type: "text/markdown" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}

function HandoffModal({ project, onClose }: { project: ProjectDetailType; onClose: () => void }) {
  const allFeatureIds = new Set((project.features ?? []).map((f) => f.id));
  const [selected, setSelected] = useState<Set<number>>(allFeatureIds);
  const [showDetails, setShowDetails] = useState(false);

  useEffect(() => {
    setSelected(new Set((project.features ?? []).map((f) => f.id)));
  }, [project.features]);

  const doExport = () => {
    const md = generateHandoffMarkdown(project, selected);
    downloadMarkdown(md, `${project.slug}-handoff.md`);
    onClose();
  };

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  const toggleAll = () => {
    if (selected.size === allFeatureIds.size) {
      setSelected(new Set());
    } else {
      setSelected(new Set(allFeatureIds));
    }
  };

  const totalFeatures = allFeatureIds.size;

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-lg rounded-xl border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="p-5 border-b border-zinc-800">
          <h2 className="text-lg font-semibold text-zinc-100">Handoff</h2>
          <p className="mt-2 text-sm text-zinc-400 leading-relaxed">
            Download a markdown file with <strong>{project.name}</strong>'s full context
            — problem statement, description, and all {totalFeatures} features with their
            statuses. Hand it to another agent so they understand exactly what this
            project is, why it exists, and what's been built.
          </p>
        </div>

        <div className="border-b border-zinc-800">
          <button
            type="button"
            onClick={() => setShowDetails(!showDetails)}
            className="w-full px-5 py-3 text-sm text-left text-zinc-300 hover:bg-zinc-800/40 flex items-center justify-between"
          >
            <span>Select details ({selected.size}/{totalFeatures} features)</span>
            <svg
              className={`w-4 h-4 text-zinc-500 transition-transform ${showDetails ? "rotate-180" : ""}`}
              fill="none"
              viewBox="0 0 24 24"
              stroke="currentColor"
              strokeWidth={2}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {showDetails && (
            <div className="px-5 pb-4">
              <div className="flex justify-end mb-2">
                <button
                  type="button"
                  onClick={toggleAll}
                  className="text-xs text-blue-400 hover:text-blue-300"
                >
                  {selected.size === allFeatureIds.size ? "Deselect all" : "Select all"}
                </button>
              </div>
              <div className="max-h-52 overflow-y-auto space-y-1 rounded border border-zinc-800 bg-zinc-950/50 p-2">
                {(project.features ?? []).map((f) => (
                  <label
                    key={f.id}
                    className="flex items-start gap-2 px-2 py-1.5 rounded hover:bg-zinc-800/60 cursor-pointer"
                  >
                    <input
                      type="checkbox"
                      checked={selected.has(f.id)}
                      onChange={() => toggle(f.id)}
                      className="mt-0.5 accent-blue-500"
                    />
                    <div className="min-w-0">
                      <div className="text-sm text-zinc-200 truncate">{f.title}</div>
                      <div className="text-xs text-zinc-500">{STATUS_LABEL[f.status]}</div>
                    </div>
                  </label>
                ))}
              </div>
            </div>
          )}
        </div>

        <div className="p-4 flex justify-end gap-3">
          <button
            type="button"
            onClick={onClose}
            className="px-4 py-2 text-sm rounded border border-zinc-700 text-zinc-400 hover:bg-zinc-800 transition-colors"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={doExport}
            className="px-4 py-2 text-sm rounded border border-blue-600/50 bg-blue-900/30 text-blue-300 hover:bg-blue-800/40 transition-colors"
          >
            Download
          </button>
        </div>
      </div>
    </div>
  );
}

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
  const queryClient = useQueryClient();

  const { data, isLoading, error } = useQuery({
    queryKey: ["project", slug],
    queryFn: () => api.getProject(slug),
    enabled: slug.length > 0,
  });

  // ----- mutations ----------------------------------------------------

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["project", slug] });
    queryClient.invalidateQueries({ queryKey: ["projects"] });
  };

  const updateProject = useMutation({
    mutationFn: (body: Parameters<typeof api.updateProject>[1]) =>
      api.updateProject(slug, body),
    onSuccess: invalidate,
  });

  const createFeature = useMutation({
    mutationFn: (title: string) => api.createFeature(slug, { title }),
    onSuccess: invalidate,
  });

  const updateFeature = useMutation({
    mutationFn: ({ id, body }: { id: number; body: Parameters<typeof api.updateFeature>[1] }) =>
      api.updateFeature(id, body),
    onSuccess: invalidate,
  });

  const deleteFeature = useMutation({
    mutationFn: (id: number) => api.deleteFeature(id),
    onSuccess: invalidate,
  });

  const logEffort = useMutation({
    mutationFn: (body: Parameters<typeof api.logEffort>[1]) =>
      api.logEffort(slug, body),
    onSuccess: invalidate,
  });

  // ----- local UI state -----------------------------------------------

  const [newFeatureTitle, setNewFeatureTitle] = useState("");
  const [effortMinutes, setEffortMinutes] = useState("");
  const [effortNote, setEffortNote] = useState("");
  const [editingProblem, setEditingProblem] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [showHandoff, setShowHandoff] = useState(false);

  // ----- render --------------------------------------------------------

  if (isLoading) {
    return (
      <main className="min-h-screen bg-neutral-950 text-neutral-100 p-8">
        <p className="text-neutral-400">loading…</p>
      </main>
    );
  }

  if (error) {
    return (
      <main className="min-h-screen bg-neutral-950 text-neutral-100 p-8">
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">
          ← back
        </Link>
        <div className="mt-4 rounded border border-red-500/40 bg-red-500/10 p-4 text-red-300">
          failed to load project: {(error as Error).message}
        </div>
      </main>
    );
  }

  if (!data) return null;

  const features = data.features ?? [];
  const effort = data.effort ?? [];
  const groups = groupByStatus(features);
  const totalEffort = features.length;
  const doneCount = groups.done.length;

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100">
      <div className="max-w-7xl mx-auto p-6 lg:p-8">
        <Link
          to="/"
          className="text-sm text-neutral-400 hover:text-neutral-200"
        >
          ← back
        </Link>

        {/* Header */}
        <header className="mt-4 flex flex-wrap items-center gap-3">
          <span
            className="h-5 w-5 rounded-full flex-shrink-0"
            style={{ backgroundColor: data.color }}
            aria-hidden
          />
          {editingName ? (
            <InlineEdit
              initial={data.name}
              onSave={(v) => {
                if (v && v !== data.name) updateProject.mutate({ name: v });
                setEditingName(false);
              }}
              onCancel={() => setEditingName(false)}
              className="text-3xl font-semibold tracking-tight bg-neutral-900 border border-neutral-700 rounded px-2 py-0.5"
            />
          ) : (
            <h1
              className="text-3xl font-semibold tracking-tight cursor-text"
              onClick={() => setEditingName(true)}
              title="click to rename"
            >
              {data.name}
            </h1>
          )}
          <button
            type="button"
            onClick={() =>
              updateProject.mutate({ priority: PRIORITY_NEXT[data.priority] })
            }
            className="cursor-pointer"
            title="click to cycle priority"
          >
            <PriorityPill priority={data.priority} />
          </button>
          <span className="text-sm text-neutral-500">
            created {formatDate(data.created_at)}
          </span>
          <span className="text-sm text-neutral-500">
            · {formatHours(data.total_minutes)} logged
          </span>
          <span className="text-sm text-neutral-500">
            · {doneCount}/{totalEffort} done
          </span>
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              onClick={() => setShowHandoff(true)}
              className="px-3 py-1.5 text-sm rounded border border-blue-600/50 bg-blue-900/30 text-blue-300 hover:bg-blue-800/40 hover:border-blue-500/60 transition-colors"
            >
              Handoff
            </button>
            <span
              className="px-3 py-1.5 text-sm rounded border border-zinc-700/40 bg-zinc-800/30 text-zinc-600 cursor-not-allowed"
              title="Coming soon"
            >
              Work on it
            </span>
          </div>
        </header>

        {/* Problem block */}
        <section className="mt-6 max-w-3xl">
          <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-1">
            problem
          </h2>
          {editingProblem ? (
            <InlineEdit
              multiline
              initial={data.problem}
              onSave={(v) => {
                if (v.trim() && v !== data.problem)
                  updateProject.mutate({ problem: v });
                setEditingProblem(false);
              }}
              onCancel={() => setEditingProblem(false)}
              className="w-full bg-neutral-900 border border-neutral-700 rounded p-3 text-neutral-200 leading-relaxed min-h-[120px]"
            />
          ) : (
            <blockquote
              className="border-l-2 border-neutral-700 pl-4 text-neutral-300 italic leading-relaxed cursor-text hover:border-neutral-500"
              onClick={() => setEditingProblem(true)}
              title="click to edit"
            >
              {data.problem}
            </blockquote>
          )}
        </section>

        {/* Add feature */}
        <section className="mt-8 max-w-3xl">
          <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-2">
            add feature
          </h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              if (!newFeatureTitle.trim()) return;
              createFeature.mutate(newFeatureTitle.trim(), {
                onSuccess: () => setNewFeatureTitle(""),
              });
            }}
            className="flex gap-2"
          >
            <input
              type="text"
              value={newFeatureTitle}
              onChange={(e) => setNewFeatureTitle(e.target.value)}
              placeholder="what needs to happen?"
              className="flex-1 bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm focus:border-neutral-500 focus:outline-none"
            />
            <button
              type="submit"
              disabled={!newFeatureTitle.trim() || createFeature.isPending}
              className="px-4 py-2 text-sm rounded border border-neutral-700 bg-neutral-900 hover:bg-neutral-800 disabled:opacity-40"
            >
              add
            </button>
          </form>
        </section>

        {/* Four-column board */}
        <section className="mt-8">
          <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-3">
            features ({features.length})
          </h2>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
            {STATUS_ORDER.map((status) => (
              <FeatureColumn
                key={status}
                status={status}
                features={groups[status]}
                onCycleStatus={(f) =>
                  updateFeature.mutate({
                    id: f.id,
                    body: { status: STATUS_NEXT[f.status] },
                  })
                }
                onDelete={(f) => {
                  if (confirm(`Delete "${f.title}"?`)) deleteFeature.mutate(f.id);
                }}
              />
            ))}
          </div>
        </section>

        {/* Effort log */}
        <section className="mt-10 max-w-3xl">
          <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-2">
            log effort
          </h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const minutes = parseInt(effortMinutes, 10);
              if (!minutes || minutes <= 0) return;
              logEffort.mutate(
                { minutes, note: effortNote.trim() || undefined },
                {
                  onSuccess: () => {
                    setEffortMinutes("");
                    setEffortNote("");
                  },
                },
              );
            }}
            className="flex gap-2 items-center"
          >
            <input
              type="number"
              value={effortMinutes}
              onChange={(e) => setEffortMinutes(e.target.value)}
              placeholder="min"
              min="1"
              className="w-20 bg-neutral-900 border border-neutral-700 rounded px-2 py-2 text-sm focus:border-neutral-500 focus:outline-none"
            />
            <input
              type="text"
              value={effortNote}
              onChange={(e) => setEffortNote(e.target.value)}
              placeholder="what did you work on?"
              className="flex-1 bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm focus:border-neutral-500 focus:outline-none"
            />
            <button
              type="submit"
              disabled={!effortMinutes || logEffort.isPending}
              className="px-4 py-2 text-sm rounded border border-neutral-700 bg-neutral-900 hover:bg-neutral-800 disabled:opacity-40"
            >
              log
            </button>
          </form>

          {effort.length > 0 && (
            <div className="mt-4">
              <EffortSparkline effort={effort} />
              <ul className="mt-3 space-y-1 text-sm text-neutral-400">
                {effort
                  .slice()
                  .sort((a, b) => b.logged_at - a.logged_at)
                  .slice(0, 10)
                  .map((e) => (
                    <li key={e.id}>
                      <span className="text-neutral-500">
                        {formatRelative(e.logged_at)}
                      </span>{" "}
                      <span className="tabular-nums">{e.minutes}m</span>
                      {e.note ? <span> — {e.note}</span> : null}
                    </li>
                  ))}
              </ul>
            </div>
          )}
        </section>
      </div>

      {showHandoff && (
        <HandoffModal project={data} onClose={() => setShowHandoff(false)} />
      )}
    </main>
  );
}

// ----- subcomponents -------------------------------------------------------

function FeatureColumn({
  status,
  features,
  onCycleStatus,
  onDelete,
}: {
  status: Status;
  features: Feature[];
  onCycleStatus: (f: Feature) => void;
  onDelete: (f: Feature) => void;
}) {
  const [expanded, setExpanded] = useState<number | null>(null);

  return (
    <div>
      <h3 className="text-xs uppercase tracking-wider text-neutral-500 mb-2 flex items-center gap-2">
        <span className={`h-2 w-2 rounded-full ${STATUS_DOT[status]}`} />
        {STATUS_LABEL[status]} ({features.length})
      </h3>
      <ul className="space-y-2">
        {features.length === 0 && (
          <li className="text-xs text-neutral-600 italic px-2">empty</li>
        )}
        {features.map((f) => (
          <li
            key={f.id}
            className={`group relative rounded border ${STATUS_BORDER[f.status]} bg-neutral-900/50 px-3 py-2 text-sm cursor-pointer`}
            onClick={() => setExpanded(expanded === f.id ? null : f.id)}
          >
            <div className="text-neutral-200">{f.title}</div>
            {expanded === f.id && f.description && (
              <p className="mt-2 text-xs text-neutral-400 leading-relaxed border-t border-neutral-800 pt-2">
                {f.description}
              </p>
            )}
            <div className="mt-1 flex items-center justify-between text-xs text-neutral-500">
              <span>{formatRelative(f.created_at)}</span>
              <div className="flex gap-1 opacity-0 group-hover:opacity-100 transition-opacity">
                <button
                  type="button"
                  onClick={(e) => { e.stopPropagation(); onCycleStatus(f); }}
                  className="px-1.5 py-0.5 rounded border border-neutral-700 hover:bg-neutral-800"
                  title={`move to ${STATUS_NEXT[f.status]}`}
                >
                  →
                </button>
                <button
                  type="button"
                  onClick={(e) => { e.stopPropagation(); onDelete(f); }}
                  className="px-1.5 py-0.5 rounded border border-neutral-700 hover:bg-red-900/50"
                  title="delete"
                >
                  ×
                </button>
              </div>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

function EffortSparkline({
  effort,
}: {
  effort: { logged_at: number; minutes: number }[];
}) {
  if (effort.length === 0) return null;
  // Bin by day. The sparkline is purely visual — exact timestamps are
  // in the list below.
  const sorted = effort.slice().sort((a, b) => a.logged_at - b.logged_at);
  const dayMs = 86400;
  const start = Math.floor(sorted[0].logged_at / dayMs);
  const end = Math.floor(Date.now() / 1000 / dayMs);
  const days = Math.max(end - start + 1, 1);
  const bins = new Array(days).fill(0) as number[];
  for (const e of sorted) {
    const idx = Math.floor(e.logged_at / dayMs) - start;
    bins[idx] = (bins[idx] ?? 0) + e.minutes;
  }
  const max = Math.max(...bins, 1);
  const W = 320;
  const H = 40;
  const barW = W / days;
  return (
    <svg width={W} height={H} className="text-neutral-500">
      {bins.map((v, i) => {
        const h = (v / max) * H;
        return (
          <rect
            key={i}
            x={i * barW}
            y={H - h}
            width={Math.max(barW - 1, 1)}
            height={h}
            fill="currentColor"
            opacity={v ? 0.8 : 0.15}
          />
        );
      })}
    </svg>
  );
}

function InlineEdit({
  initial,
  onSave,
  onCancel,
  className,
  multiline = false,
}: {
  initial: string;
  onSave: (v: string) => void;
  onCancel: () => void;
  className?: string;
  multiline?: boolean;
}) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement | HTMLTextAreaElement>(null);

  useEffect(() => {
    ref.current?.focus();
    if (ref.current && "select" in ref.current) {
      (ref.current as HTMLInputElement).select?.();
    }
  }, []);

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === "Escape") {
      e.preventDefault();
      onCancel();
    }
    if (e.key === "Enter" && !multiline) {
      e.preventDefault();
      onSave(value);
    }
    if (e.key === "Enter" && multiline && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      onSave(value);
    }
  };

  if (multiline) {
    return (
      <textarea
        ref={ref as React.RefObject<HTMLTextAreaElement>}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={() => onSave(value)}
        onKeyDown={handleKey}
        className={className}
        rows={5}
      />
    );
  }
  return (
    <input
      ref={ref as React.RefObject<HTMLInputElement>}
      type="text"
      value={value}
      onChange={(e) => setValue(e.target.value)}
      onBlur={() => onSave(value)}
      onKeyDown={handleKey}
      className={className}
    />
  );
}
