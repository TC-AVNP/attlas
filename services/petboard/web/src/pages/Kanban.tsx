import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Project, Stage, Interest } from "../api/types";

const STAGES: { key: Stage; label: string }[] = [
  { key: "idea", label: "Idea" },
  { key: "live", label: "Live" },
  { key: "completed", label: "Completed" },
];

const INTEREST_EMOJI: Record<Interest, string> = {
  excited: "🔥",
  meh: "😐",
  bored: "💤",
};

const INTEREST_ORDER: Interest[] = ["excited", "meh", "bored"];

function sortByInterest(a: Project, b: Project): number {
  return INTEREST_ORDER.indexOf(a.interest) - INTEREST_ORDER.indexOf(b.interest);
}

function ProjectCard({ project }: { project: Project }) {
  const done = project.feature_counts?.done ?? 0;
  const total = Object.values(project.feature_counts ?? {}).reduce(
    (s, n) => s + n,
    0,
  );

  return (
    <Link
      to={`/p/${project.slug}`}
      className="block rounded-lg border border-zinc-700/50 bg-zinc-800/60 p-3 hover:border-zinc-500/60 transition-colors"
    >
      <div className="flex items-center gap-2 mb-1">
        <span
          className="inline-block w-2.5 h-2.5 rounded-full shrink-0"
          style={{ backgroundColor: project.color }}
        />
        <span className="font-medium text-sm text-zinc-100 truncate">
          {project.name}
        </span>
        <span
          className="ml-auto text-xs shrink-0"
          title={project.interest}
        >
          {INTEREST_EMOJI[project.interest]}
        </span>
      </div>
      {total > 0 && (
        <div className="text-xs text-zinc-500">
          {done}/{total} features
        </div>
      )}
    </Link>
  );
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
      <div className="flex items-center gap-2 mb-3 px-1">
        <h2 className="text-sm font-semibold text-zinc-400 uppercase tracking-wider">
          {stage.label}
        </h2>
        <span className="text-xs text-zinc-600 bg-zinc-800 rounded-full px-2 py-0.5">
          {projects.length}
        </span>
      </div>
      <div className="flex flex-col gap-2">
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

type FilterInterest = Interest | "all";

export default function Kanban() {
  const qc = useQueryClient();
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

  const interestFilter: FilterInterest = "all";

  const filtered =
    interestFilter === "all"
      ? projects
      : projects.filter((p) => p.interest === interestFilter);

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
        Loading…
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-zinc-900 text-zinc-100">
      {/* Header */}
      <div className="border-b border-zinc-800 px-4 py-3">
        <div className="max-w-6xl mx-auto flex items-center justify-between">
          <h1 className="text-lg font-bold tracking-tight">petboard</h1>
          <div className="flex items-center gap-3">
            <Link
              to="/todos"
              className="text-xs text-zinc-500 hover:text-zinc-300"
            >
              todos
            </Link>
          </div>
        </div>
      </div>

      {/* Kanban board */}
      <div className="max-w-6xl mx-auto p-4">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
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
