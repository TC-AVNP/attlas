import { useState } from "react";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Project, Stage, Interest } from "../api/types";
import { ProjectCard } from "../components/ProjectCard";

type InterestFilter = Interest | "all";

const STAGES: { key: Stage; label: string }[] = [
  { key: "idea", label: "Idea" },
  { key: "live", label: "Live" },
];

const INTEREST_ORDER: Interest[] = ["excited", "meh", "bored"];

function sortByInterest(a: Project, b: Project): number {
  return INTEREST_ORDER.indexOf(a.interest) - INTEREST_ORDER.indexOf(b.interest);
}

function KanbanColumn({
  stage,
  projects,
  onDrop,
}: {
  stage: { key: Stage; label: string };
  projects: Project[];
  onDrop: (slug: string, newStage: Stage) => void;
}) {
  return (
    <div
      className="flex flex-col min-w-0 flex-1"
      onDragOver={(e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
      }}
      onDrop={(e) => {
        e.preventDefault();
        const slug = e.dataTransfer.getData("text/plain");
        if (slug) onDrop(slug, stage.key);
      }}
    >
      {/* Column header */}
      <div className="flex items-center gap-2 mb-4 px-0.5">
        <h2 className="m-0 text-[13px] font-semibold tracking-wide uppercase text-zinc-400">
          {stage.label}
        </h2>
        <span
          className="text-xs px-2 py-0.5 rounded-full text-zinc-100"
          style={{ background: "#27272A" }}
        >
          {projects.length}
        </span>
      </div>

      {/* Cards */}
      <div className="flex flex-col gap-3">
        {projects.sort(sortByInterest).map((p) => (
          <div
            key={p.slug}
            draggable
            onDragStart={(e) => {
              e.dataTransfer.setData("text/plain", p.slug);
              e.dataTransfer.effectAllowed = "move";
            }}
            className="cursor-grab active:cursor-grabbing"
          >
            <ProjectCard project={p} />
          </div>
        ))}
      </div>
    </div>
  );
}

const FILTER_OPTIONS: { key: InterestFilter; label: string }[] = [
  { key: "all", label: "All" },
  { key: "excited", label: "Excited" },
  { key: "meh", label: "Meh" },
  { key: "bored", label: "Bored" },
];

export default function Kanban() {
  const qc = useQueryClient();
  const [filter, setFilter] = useState<InterestFilter>("all");
  const { data, isLoading } = useQuery({
    queryKey: ["projects", false],
    queryFn: () => api.listProjects(false),
  });

  const mutation = useMutation({
    mutationFn: ({ slug, stage }: { slug: string; stage: Stage }) =>
      api.updateProject(slug, { stage }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["projects"] }),
  });

  const projects = data?.projects ?? [];
  const filtered = filter === "all" ? projects : projects.filter((p) => p.interest === filter);

  const byStage = (stage: Stage) =>
    filtered.filter((p) => p.stage === stage);

  const handleDrop = (slug: string, newStage: Stage) => {
    const project = projects.find((p) => p.slug === slug);
    if (project && project.stage !== newStage) {
      mutation.mutate({ slug, stage: newStage });
    }
  };

  if (isLoading) {
    return (
      <div className="flex items-center justify-center h-screen text-zinc-500">
        Loading...
      </div>
    );
  }

  return (
    <div className="min-h-screen" style={{ background: "#121214", fontFamily: "'Inter', system-ui, sans-serif" }}>
      {/* Header */}
      <div className="border-b px-4 py-3" style={{ borderColor: "#27272A" }}>
        <div className="max-w-[1080px] mx-auto flex items-center justify-between">
          <div className="flex items-center gap-6">
            <h1 className="m-0 text-xl font-bold text-green-400" style={{ letterSpacing: "-0.01em" }}>
              petboard
            </h1>
            <nav className="flex items-center gap-1">
              <span className="text-[13px] font-medium px-3 py-1.5 rounded-full border border-green-500/40 text-green-400 bg-green-500/10">
                Active
              </span>
              <Link
                to="/completed"
                className="text-[13px] font-medium px-3 py-1.5 rounded-full no-underline transition-colors border border-zinc-700 text-zinc-400 hover:border-zinc-600 hover:text-zinc-300"
              >
                Completed
              </Link>
            </nav>
          </div>
          <Link to="/todos" className="text-[13px] font-medium text-zinc-400 no-underline hover:text-zinc-300 transition-colors">
            Todos
          </Link>
        </div>
      </div>

      {/* Interest filter */}
      <div className="max-w-[1080px] mx-auto px-4 pt-5 pb-1">
        <div className="flex items-center gap-2">
          {FILTER_OPTIONS.map((opt) => (
            <button
              key={opt.key}
              type="button"
              onClick={() => setFilter(opt.key)}
              className={`text-xs font-medium px-3 py-1.5 rounded-full border transition-colors ${
                filter === opt.key
                  ? "border-green-500/40 text-green-400 bg-green-500/10"
                  : "border-zinc-700 text-zinc-400 hover:border-zinc-600 hover:text-zinc-300"
              }`}
              style={{ background: filter === opt.key ? undefined : "transparent" }}
            >
              {opt.label}
            </button>
          ))}
        </div>
      </div>

      {/* Kanban board */}
      <div className="max-w-[1080px] mx-auto px-4 py-4">
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          {STAGES.map((stage) => (
            <KanbanColumn
              key={stage.key}
              stage={stage}
              projects={byStage(stage.key)}
              onDrop={handleDrop}
            />
          ))}
        </div>
      </div>
    </div>
  );
}
