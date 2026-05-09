// ProjectDetail is the per-project page at /petboard/p/:slug.
//
// Three tabs:
//   1. Overview — problem, description, screenshot, stats
//   2. Backlog  — four-column kanban board + add-feature form
//   3. Details  — project notes with human/LLM toggle + mermaid diagrams

import { useState, useRef, useEffect } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Feature, Priority, Status, ProjectDetail as ProjectDetailType } from "../api/types";
import PriorityPill from "../components/PriorityPill";
import Markdown from "../components/Markdown";
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

type Tab = "overview" | "backlog" | "details";

const TABS: { id: Tab; label: string }[] = [
  { id: "overview", label: "Overview" },
  { id: "backlog", label: "Backlog" },
  { id: "details", label: "Project Details" },
];

// ----- main component -------------------------------------------------------

export default function ProjectDetail() {
  const { slug = "" } = useParams();
  const queryClient = useQueryClient();
  const [tab, setTab] = useState<Tab>("overview");

  const { data, isLoading, error } = useQuery({
    queryKey: ["project", slug],
    queryFn: () => api.getProject(slug),
    enabled: slug.length > 0,
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["project", slug] });
    queryClient.invalidateQueries({ queryKey: ["projects"] });
  };

  const updateProject = useMutation({
    mutationFn: (body: Parameters<typeof api.updateProject>[1]) =>
      api.updateProject(slug, body),
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

  const [editingProblem, setEditingProblem] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [showHandoff, setShowHandoff] = useState(false);

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
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">← back</Link>
        <div className="mt-4 rounded border border-red-500/40 bg-red-500/10 p-4 text-red-300">
          failed to load project: {(error as Error).message}
        </div>
      </main>
    );
  }

  if (!data) return null;

  const features = data.features ?? [];
  const groups = groupByStatus(features);

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100">
      <div className="max-w-7xl mx-auto p-6 lg:p-8">
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">← back</Link>

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
            onClick={() => updateProject.mutate({ priority: PRIORITY_NEXT[data.priority] })}
            className="cursor-pointer"
            title="click to cycle priority"
          >
            <PriorityPill priority={data.priority} />
          </button>
          <div className="ml-auto flex items-center gap-2">
            <button
              type="button"
              onClick={() => setShowHandoff(true)}
              className="px-3 py-1.5 text-sm rounded border border-blue-600/50 bg-blue-900/30 text-blue-300 hover:bg-blue-800/40 hover:border-blue-500/60 transition-colors"
            >
              Handoff
            </button>
          </div>
        </header>

        {/* Tabs */}
        <nav className="mt-6 flex gap-1 border-b border-neutral-800">
          {TABS.map((t) => (
            <button
              key={t.id}
              type="button"
              onClick={() => setTab(t.id)}
              className={`px-4 py-2.5 text-sm font-medium transition-colors relative ${
                tab === t.id
                  ? "text-neutral-100"
                  : "text-neutral-500 hover:text-neutral-300"
              }`}
            >
              {t.label}
              {t.id === "backlog" && (
                <span className="ml-1.5 text-xs text-neutral-600">
                  {features.length}
                </span>
              )}
              {tab === t.id && (
                <span
                  className="absolute bottom-0 left-0 right-0 h-0.5 rounded-full"
                  style={{ backgroundColor: data.color }}
                />
              )}
            </button>
          ))}
        </nav>

        {/* Tab content */}
        <div className="mt-6">
          {tab === "overview" && (
            <OverviewTab
              data={data}
              features={features}
              groups={groups}
              editingProblem={editingProblem}
              setEditingProblem={setEditingProblem}
              updateProject={updateProject}
            />
          )}

          {tab === "backlog" && (
            <BacklogTab
              groups={groups}
              updateFeature={updateFeature}
              deleteFeature={deleteFeature}
            />
          )}

          {tab === "details" && <DetailsTab data={data} />}
        </div>
      </div>

      {showHandoff && (
        <HandoffModal project={data} onClose={() => setShowHandoff(false)} />
      )}
    </main>
  );
}

// ----- tab components --------------------------------------------------------

function OverviewTab({
  data,
  features,
  groups,
  editingProblem,
  setEditingProblem,
  updateProject,
}: {
  data: ProjectDetailType;
  features: Feature[];
  groups: Record<Status, Feature[]>;
  editingProblem: boolean;
  setEditingProblem: (v: boolean) => void;
  updateProject: { mutate: (body: any) => void };
}) {
  return (
    <div className="max-w-3xl space-y-8">
      {/* Problem statement */}
      <section>
        <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-1">
          Problem
        </h2>
        {editingProblem ? (
          <InlineEdit
            multiline
            initial={data.problem}
            onSave={(v) => {
              if (v.trim() && v !== data.problem) updateProject.mutate({ problem: v });
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

      {/* What is this project + screenshot */}
      <section>
        <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-2">
          What is this project
        </h2>
        <div className={data.screenshot_url ? "flex gap-6 items-start" : ""}>
          <div className="flex-1">
            {data.description ? (
              <Markdown content={data.description} />
            ) : (
              <p className="text-neutral-600 italic">No description yet.</p>
            )}
          </div>
          {data.screenshot_url && (
            <div className="flex-shrink-0 w-64">
              <img
                src={data.screenshot_url}
                alt={`${data.name} screenshot`}
                className="rounded border border-neutral-800 w-full"
              />
            </div>
          )}
        </div>
      </section>

      {/* Flow */}
      {data.flow && (
        <section>
          <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-2">
            How it works
          </h2>
          <div className="rounded border border-neutral-800 bg-neutral-900/30 p-4">
            <Markdown content={data.flow} />
          </div>
        </section>
      )}

      {/* Stats */}
      <section>
        <h2 className="text-xs uppercase tracking-wider text-neutral-500 mb-3">
          Stats
        </h2>
        <div className="grid grid-cols-2 sm:grid-cols-4 gap-4">
          <StatCard label="Total features" value={features.length} />
          <StatCard label="Done" value={groups.done.length} color="text-emerald-400" />
          <StatCard label="In progress" value={groups.in_progress.length} color="text-amber-400" />
          <StatCard label="Backlog" value={groups.backlog.length} />
          <StatCard label="Dropped" value={groups.dropped.length} color="text-neutral-600" />
          <StatCard label="Time logged" value={formatHours(data.total_minutes)} />
          <StatCard label="Created" value={formatDate(data.created_at)} />
          <StatCard label="Stage" value={data.stage} />
          {data.loc?.total && (
            <StatCard label="Lines of code" value={data.loc.total.toLocaleString()} />
          )}
        </div>
      </section>
    </div>
  );
}

function StatCard({
  label,
  value,
  color,
}: {
  label: string;
  value: string | number;
  color?: string;
}) {
  return (
    <div className="rounded border border-neutral-800 bg-neutral-900/30 p-3">
      <div className="text-xs text-neutral-500 uppercase tracking-wider">{label}</div>
      <div className={`mt-1 text-lg font-semibold ${color || "text-neutral-200"}`}>
        {value}
      </div>
    </div>
  );
}

function BacklogTab({
  groups,
  updateFeature,
  deleteFeature,
}: {
  groups: Record<Status, Feature[]>;
  updateFeature: { mutate: (args: { id: number; body: any }) => void };
  deleteFeature: { mutate: (id: number) => void };
}) {
  return (
    <div>
      {/* Four-column board */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
        {STATUS_ORDER.map((status) => (
          <FeatureColumn
            key={status}
            status={status}
            features={groups[status]}
            onCycleStatus={(f) =>
              updateFeature.mutate({ id: f.id, body: { status: STATUS_NEXT[f.status] } })
            }
            onDelete={(f) => {
              if (confirm(`Delete "${f.title}"?`)) deleteFeature.mutate(f.id);
            }}
          />
        ))}
      </div>
    </div>
  );
}

function DetailsTab({ data }: { data: ProjectDetailType }) {
  const [view, setView] = useState<"human" | "llm">("human");

  const notes = view === "human" ? data.notes : data.notes_llm;
  const hasHuman = !!data.notes;
  const hasLLM = !!data.notes_llm;

  return (
    <div className="max-w-4xl">
      {/* View toggle */}
      {(hasHuman || hasLLM) && (
        <div className="flex gap-1 mb-4">
          <button
            type="button"
            onClick={() => setView("human")}
            className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
              view === "human"
                ? "border-blue-500/50 bg-blue-900/30 text-blue-300"
                : "border-neutral-700 text-neutral-500 hover:text-neutral-300"
            }`}
          >
            Human
          </button>
          <button
            type="button"
            onClick={() => setView("llm")}
            className={`px-3 py-1.5 text-xs rounded-full border transition-colors ${
              view === "llm"
                ? "border-purple-500/50 bg-purple-900/30 text-purple-300"
                : "border-neutral-700 text-neutral-500 hover:text-neutral-300"
            }`}
          >
            LLM
          </button>
        </div>
      )}

      {notes ? (
        <div className="rounded border border-neutral-800 bg-neutral-900/30 p-6">
          <Markdown content={notes} />
        </div>
      ) : (
        <p className="text-neutral-600 italic">
          No {view === "human" ? "human-readable" : "LLM"} project details yet.
        </p>
      )}
    </div>
  );
}

// ----- subcomponents ---------------------------------------------------------

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
    if (e.key === "Escape") { e.preventDefault(); onCancel(); }
    if (e.key === "Enter" && !multiline) { e.preventDefault(); onSave(value); }
    if (e.key === "Enter" && multiline && (e.metaKey || e.ctrlKey)) { e.preventDefault(); onSave(value); }
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

// ----- handoff modal ---------------------------------------------------------

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
  const features = (project.features ?? []).filter((f) => selectedFeatureIds.has(f.id));
  if (features.length > 0) {
    lines.push(`## Features\n`);
    for (const status of STATUS_ORDER) {
      const group = features.filter((f) => f.status === status);
      if (group.length === 0) continue;
      lines.push(`### ${STATUS_LABEL[status]} (${group.length})\n`);
      for (const f of group) {
        lines.push(`- **${f.title}**`);
        if (f.description) lines.push(`  ${f.description}`);
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
    if (selected.size === allFeatureIds.size) setSelected(new Set());
    else setSelected(new Set(allFeatureIds));
  };

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60"
      onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
    >
      <div className="w-full max-w-lg rounded-xl border border-zinc-700 bg-zinc-900 shadow-2xl">
        <div className="p-5 border-b border-zinc-800">
          <h2 className="text-lg font-semibold text-zinc-100">Handoff</h2>
          <p className="mt-2 text-sm text-zinc-400 leading-relaxed">
            Download a markdown file with <strong>{project.name}</strong>'s full context.
          </p>
        </div>
        <div className="border-b border-zinc-800">
          <button
            type="button"
            onClick={() => setShowDetails(!showDetails)}
            className="w-full px-5 py-3 text-sm text-left text-zinc-300 hover:bg-zinc-800/40 flex items-center justify-between"
          >
            <span>Select features ({selected.size}/{allFeatureIds.size})</span>
            <svg
              className={`w-4 h-4 text-zinc-500 transition-transform ${showDetails ? "rotate-180" : ""}`}
              fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={2}
            >
              <path strokeLinecap="round" strokeLinejoin="round" d="M19 9l-7 7-7-7" />
            </svg>
          </button>
          {showDetails && (
            <div className="px-5 pb-4">
              <div className="flex justify-end mb-2">
                <button type="button" onClick={toggleAll} className="text-xs text-blue-400 hover:text-blue-300">
                  {selected.size === allFeatureIds.size ? "Deselect all" : "Select all"}
                </button>
              </div>
              <div className="max-h-52 overflow-y-auto space-y-1 rounded border border-zinc-800 bg-zinc-950/50 p-2">
                {(project.features ?? []).map((f) => (
                  <label key={f.id} className="flex items-start gap-2 px-2 py-1.5 rounded hover:bg-zinc-800/60 cursor-pointer">
                    <input type="checkbox" checked={selected.has(f.id)} onChange={() => toggle(f.id)} className="mt-0.5 accent-blue-500" />
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
          <button type="button" onClick={onClose} className="px-4 py-2 text-sm rounded border border-zinc-700 text-zinc-400 hover:bg-zinc-800 transition-colors">Cancel</button>
          <button type="button" onClick={doExport} className="px-4 py-2 text-sm rounded border border-blue-600/50 bg-blue-900/30 text-blue-300 hover:bg-blue-800/40 transition-colors">Download</button>
        </div>
      </div>
    </div>
  );
}

function groupByStatus(features: Feature[]): Record<Status, Feature[]> {
  const groups: Record<Status, Feature[]> = { backlog: [], in_progress: [], done: [], dropped: [] };
  for (const f of features) groups[f.status].push(f);
  return groups;
}
