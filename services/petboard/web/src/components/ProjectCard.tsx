import { Link } from "react-router-dom";
import type { Project, Interest } from "../api/types";
import { formatHours } from "../lib/format";

export function InterestIcon({ interest }: { interest: Interest }) {
  const color = "#3F3F46";
  if (interest === "excited") {
    return (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2" />
      </svg>
    );
  }
  if (interest === "meh") {
    return (
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
        <circle cx="12" cy="12" r="10" />
        <line x1="8" y1="15" x2="16" y2="15" />
        <line x1="9" y1="9" x2="9.01" y2="9" />
        <line x1="15" y1="9" x2="15.01" y2="9" />
      </svg>
    );
  }
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke={color} strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
      <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.73 21a2 2 0 0 1-3.46 0" />
      <line x1="1" y1="1" x2="23" y2="23" />
    </svg>
  );
}

export function ProjectCard({ project }: { project: Project }) {
  const counts = project.feature_counts ?? {};
  const done = counts.done ?? 0;
  const backlog = (counts.backlog ?? 0) + (counts.in_progress ?? 0);
  const isApp = !!project.screenshot_url;

  return (
    <Link
      to={`/p/${project.slug}`}
      className={`block rounded-xl p-4 no-underline text-inherit transition-colors relative overflow-hidden ${
        isApp
          ? "border border-green-500/40 hover:border-green-400/60"
          : "border border-zinc-700 hover:border-zinc-600"
      }`}
      style={{
        background: "#1C1C21",
        boxShadow: isApp ? "0 0 12px rgba(34, 197, 94, 0.08)" : undefined,
      }}
    >
      {/* Corner ribbon for app projects */}
      {isApp && (
        <div
          style={{
            position: "absolute",
            top: 2,
            left: -36,
            width: 95,
            transform: "rotate(-45deg)",
            background: "#22C55E",
            color: "#121214",
            fontSize: 9,
            fontWeight: 700,
            textTransform: "uppercase",
            letterSpacing: "0.08em",
            textAlign: "center",
            padding: "2px 0",
            boxShadow: "0 1px 4px rgba(0,0,0,0.3)",
          }}
        >
          App
        </div>
      )}

      {/* Row 1: Name + backlog count + interest icon */}
      <div className="flex justify-between items-center gap-2 mb-1">
        <span className="text-[15px] font-medium text-zinc-100 truncate min-w-0">
          {project.name}
        </span>
        <div className="flex items-center gap-2 shrink-0">
          {backlog > 0 && <span className="text-xs text-zinc-400" title="Backlog">&#x1F525; {backlog}</span>}
          <span className="mt-0.5" title={project.interest}>
            <InterestIcon interest={project.interest} />
          </span>
        </div>
      </div>

      {/* Row 2: Done count */}
      {done > 0 && (
        <div className="text-xs text-zinc-400 mb-1">
          Completed Features: <span className="text-yellow-600 font-semibold">{done}</span>
        </div>
      )}

      {/* Row 3: Hours */}
      <div className="text-xs text-zinc-400">
        Hours:{" "}
        <span className="text-green-400 font-semibold text-sm">
          {formatHours(project.total_minutes)}
        </span>
      </div>
    </Link>
  );
}
