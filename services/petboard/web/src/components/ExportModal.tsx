import { useState, useMemo } from "react";
import type { ProjectDetail, Feature } from "../api/types";

interface Props {
  projects: ProjectDetail[];
  onClose: () => void;
}

const STATUS_LABEL: Record<string, string> = {
  backlog: "backlog",
  in_progress: "in progress",
  done: "done",
  dropped: "dropped",
};

function generateMarkdown(
  project: ProjectDetail,
  selectedIds: Set<number>,
): string {
  const lines: string[] = [];

  lines.push(`# ${project.name}`);
  lines.push("");
  lines.push("## Problem");
  lines.push("");
  lines.push(project.problem);
  lines.push("");

  if (project.description) {
    lines.push("## Description");
    lines.push("");
    lines.push(project.description);
    lines.push("");
  }

  const selected = project.features.filter((f) => selectedIds.has(f.id));
  if (selected.length > 0) {
    lines.push("## Features");
    lines.push("");
    for (const f of selected) {
      lines.push(`### ${f.title}`);
      lines.push("");
      lines.push(`**Status:** ${STATUS_LABEL[f.status] ?? f.status}`);
      lines.push("");
      if (f.description) {
        lines.push(f.description);
        lines.push("");
      }
    }
  }

  return lines.join("\n");
}

export default function ExportModal({ projects, onClose }: Props) {
  const [selectedSlug, setSelectedSlug] = useState<string>(
    projects[0]?.slug ?? "",
  );
  const project = projects.find((p) => p.slug === selectedSlug);

  // All features selected by default when project changes.
  const [selected, setSelected] = useState<Set<number>>(() => {
    const ids = projects[0]?.features.map((f) => f.id) ?? [];
    return new Set(ids);
  });
  const [copied, setCopied] = useState(false);

  function selectProject(slug: string) {
    setSelectedSlug(slug);
    const p = projects.find((pr) => pr.slug === slug);
    setSelected(new Set(p?.features.map((f) => f.id) ?? []));
  }

  function toggle(id: number) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function selectAll() {
    if (!project) return;
    setSelected(new Set(project.features.map((f) => f.id)));
  }

  function selectNone() {
    setSelected(new Set());
  }

  const markdown = useMemo(
    () => (project ? generateMarkdown(project, selected) : ""),
    [project, selected],
  );

  async function copyToClipboard() {
    await navigator.clipboard.writeText(markdown);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  // Group features by status for easier scanning.
  const grouped = useMemo(() => {
    if (!project) return [];
    const order: string[] = ["in_progress", "backlog", "done", "dropped"];
    const groups: { status: string; features: Feature[] }[] = [];
    for (const s of order) {
      const feats = project.features.filter((f) => f.status === s);
      if (feats.length > 0) groups.push({ status: s, features: feats });
    }
    return groups;
  }, [project]);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="bg-neutral-950 border border-neutral-800 rounded-xl shadow-2xl w-full max-w-5xl max-h-[90vh] flex flex-col mx-4">
        {/* Header */}
        <div className="flex items-center justify-between px-5 py-4 border-b border-neutral-800">
          <h2 className="text-base font-semibold">Export project as Markdown</h2>
          <button
            type="button"
            onClick={onClose}
            className="text-neutral-500 hover:text-neutral-200 text-lg leading-none px-1"
          >
            ✕
          </button>
        </div>

        {/* Body — two columns */}
        <div className="flex-1 min-h-0 flex divide-x divide-neutral-800">
          {/* Left: project selector + feature checkboxes */}
          <div className="w-2/5 flex flex-col min-h-0">
            {/* Project selector */}
            <div className="px-5 py-3 border-b border-neutral-800">
              <label className="block text-xs text-neutral-500 mb-1">
                Project
              </label>
              <select
                value={selectedSlug}
                onChange={(e) => selectProject(e.target.value)}
                className="w-full bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm focus:outline-none focus:border-neutral-500"
              >
                {projects.map((p) => (
                  <option key={p.slug} value={p.slug}>
                    {p.name}
                  </option>
                ))}
              </select>
            </div>

            {/* Feature list */}
            <div className="flex-1 overflow-y-auto px-5 py-3">
              <div className="flex items-center justify-between mb-2">
                <span className="text-xs text-neutral-500">
                  Features ({selected.size}/{project?.features.length ?? 0})
                </span>
                <span className="flex gap-2 text-xs">
                  <button
                    type="button"
                    onClick={selectAll}
                    className="text-neutral-500 hover:text-neutral-200"
                  >
                    all
                  </button>
                  <button
                    type="button"
                    onClick={selectNone}
                    className="text-neutral-500 hover:text-neutral-200"
                  >
                    none
                  </button>
                </span>
              </div>

              {grouped.map((g) => (
                <div key={g.status} className="mb-3">
                  <div className="text-[10px] uppercase tracking-wider text-neutral-600 mb-1">
                    {STATUS_LABEL[g.status] ?? g.status}
                  </div>
                  {g.features.map((f) => (
                    <label
                      key={f.id}
                      className="flex items-start gap-2 py-1 cursor-pointer group"
                    >
                      <input
                        type="checkbox"
                        checked={selected.has(f.id)}
                        onChange={() => toggle(f.id)}
                        className="mt-0.5 accent-amber-500"
                      />
                      <span className="text-sm text-neutral-300 group-hover:text-neutral-100 leading-snug">
                        {f.title}
                      </span>
                    </label>
                  ))}
                </div>
              ))}

              {project && project.features.length === 0 && (
                <p className="text-sm text-neutral-500 italic">
                  no features yet
                </p>
              )}
            </div>
          </div>

          {/* Right: markdown preview */}
          <div className="w-3/5 flex flex-col min-h-0">
            <div className="flex items-center justify-between px-5 py-3 border-b border-neutral-800">
              <span className="text-xs text-neutral-500">Preview</span>
              <button
                type="button"
                onClick={copyToClipboard}
                className={`px-3 py-1 text-xs rounded border transition-colors ${
                  copied
                    ? "border-emerald-600 bg-emerald-500/20 text-emerald-300"
                    : "border-neutral-700 bg-neutral-900 text-neutral-400 hover:text-neutral-200 hover:border-neutral-600"
                }`}
              >
                {copied ? "copied!" : "copy"}
              </button>
            </div>
            <pre className="flex-1 overflow-auto px-5 py-3 text-xs text-neutral-300 whitespace-pre-wrap font-mono leading-relaxed">
              {markdown || "Select features to preview the export."}
            </pre>
          </div>
        </div>
      </div>
    </div>
  );
}
