import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { Interest } from "../api/types";
import { ProjectCard } from "../components/ProjectCard";

const INTEREST_ORDER: Interest[] = ["excited", "meh", "bored"];

export default function Completed() {
  const { data, isLoading } = useQuery({
    queryKey: ["projects", false],
    queryFn: () => api.listProjects(false),
  });

  const projects = (data?.projects ?? [])
    .filter((p) => p.stage === "completed")
    .sort((a, b) => INTEREST_ORDER.indexOf(a.interest) - INTEREST_ORDER.indexOf(b.interest));

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
              <Link
                to="/"
                className="text-[13px] font-medium px-3 py-1.5 rounded-full no-underline transition-colors border border-zinc-700 text-zinc-400 hover:border-zinc-600 hover:text-zinc-300"
              >
                Active
              </Link>
              <span className="text-[13px] font-medium px-3 py-1.5 rounded-full border border-green-500/40 text-green-400 bg-green-500/10">
                Completed
              </span>
            </nav>
          </div>
          <Link to="/todos" className="text-[13px] font-medium text-zinc-400 no-underline hover:text-zinc-300 transition-colors">
            Todos
          </Link>
        </div>
      </div>

      {/* Grid of completed projects */}
      <div className="max-w-[1080px] mx-auto px-4 py-6">
        <div className="grid grid-cols-1 md:grid-cols-3 gap-3">
          {projects.map((p) => (
            <ProjectCard key={p.slug} project={p} />
          ))}
          {projects.length === 0 && (
            <div className="text-zinc-500 text-sm col-span-3 text-center py-12">
              No completed projects yet.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
