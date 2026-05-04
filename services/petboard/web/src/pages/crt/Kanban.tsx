import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { Project, Stage, Interest } from "../../api/types";
import { formatHours } from "../../lib/format";

const STAGES: Stage[] = ["idea", "live", "completed"];

const STAGE_META: Record<Stage, { dir: string; desc: string }> = {
  idea:      { dir: "/idea",      desc: "just an idea" },
  live:      { dir: "/live",      desc: "in production, in use" },
  completed: { dir: "/completed", desc: "done, not actively touched" },
};

const INTEREST_VU: Record<Interest, number> = {
  excited: 9,
  meh: 5,
  bored: 2,
};

const INTEREST_ORDER: Interest[] = ["excited", "meh", "bored"];

function sortByInterest(a: Project, b: Project): number {
  return INTEREST_ORDER.indexOf(a.interest) - INTEREST_ORDER.indexOf(b.interest);
}

function progressBar(done: number, total: number, width = 10): string {
  if (total === 0) return '[' + '\u2591'.repeat(width) + ']';
  const filled = Math.round((done / total) * width);
  return '[' + '\u2588'.repeat(filled) + '\u2591'.repeat(width - filled) + ']';
}

function daysSince(unixSeconds: number | undefined): number | null {
  if (!unixSeconds) return null;
  const ms = Date.now() - unixSeconds * 1000;
  return Math.max(0, Math.floor(ms / 86400000));
}

function VUMeter({ level }: { level: number }) {
  const cells = [];
  for (let i = 0; i < 10; i++) {
    let cls = 'vu-cell';
    if (i < level) {
      if (i >= 8) cls += ' peak';
      else if (i >= 6) cls += ' hot';
      else cls += ' on';
    }
    cells.push(<span key={i} className={cls} />);
  }
  return <span style={{ display: 'inline-flex' }}>{cells}</span>;
}

function CardASCII({ project }: { project: Project }) {
  const vuLevel = INTEREST_VU[project.interest];
  const counts = project.feature_counts ?? {};
  const total = Object.values(counts).reduce((s, n) => s + n, 0);
  const done = counts.done ?? 0;

  // Find latest effort timestamp from total_minutes as proxy — we don't have last_effort on the list endpoint
  // We'll show idle based on created_at as a fallback
  const idle = daysSince(project.created_at);

  return (
    <Link
      to={`/p/${project.slug}`}
      className="proj-card ascii-box"
      style={{
        display: 'block',
        padding: '10px 12px',
        background: 'var(--phos-bg-deep)',
        cursor: 'pointer',
        position: 'relative',
        textDecoration: 'none',
        color: 'inherit',
      }}
      draggable
      onDragStart={(e) => {
        e.dataTransfer.setData("text/plain", project.slug);
        e.dataTransfer.effectAllowed = "move";
      }}
    >
      {/* Top row: id + name */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline', marginBottom: 4 }}>
        <span className="fg-faint" style={{ fontSize: 10 }}>PB-{String(project.id).padStart(3, '0')}</span>
      </div>
      <div className="h-bitmap-md fg-bright" style={{ marginBottom: 6, letterSpacing: '0.02em' }}>
        {project.name}
      </div>

      {/* Problem snippet */}
      <div className="fg-dim" style={{ fontSize: 12, lineHeight: 1.4, marginBottom: 10 }}>
        {project.problem.length > 130 ? project.problem.slice(0, 128) + '\u2026' : project.problem}
      </div>

      {/* VU + progress row */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 4, fontSize: 11 }}>
        <span className="fg-dim" style={{ textTransform: 'uppercase', fontSize: 10 }}>INT</span>
        <VUMeter level={vuLevel} />
      </div>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', fontSize: 11 }}>
        <span className="fg-dim" style={{ textTransform: 'uppercase', fontSize: 10 }}>PRG</span>
        <span className="tt-mono" style={{ fontSize: 11 }}>
          {progressBar(done, total)} <span className="fg-faint">{done}/{total}</span>
        </span>
      </div>

      {/* Footer */}
      <div style={{
        display: 'flex', justifyContent: 'space-between',
        marginTop: 8, paddingTop: 6,
        borderTop: '1px dotted var(--phos-fg-faint)',
        fontSize: 10, textTransform: 'uppercase',
      }}>
        <span className="fg-dim">{formatHours(project.total_minutes)} logged</span>
        <span className="fg-faint">{idle != null ? `${idle}d` : 'untouched'}</span>
      </div>
    </Link>
  );
}

function ColumnASCII({
  stage,
  projects,
  onDrop,
}: {
  stage: Stage;
  projects: Project[];
  onDrop: (slug: string, newStage: Stage) => void;
}) {
  const meta = STAGE_META[stage];
  return (
    <div
      onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "move"; }}
      onDrop={(e) => {
        e.preventDefault();
        const slug = e.dataTransfer.getData("text/plain");
        if (slug) onDrop(slug, stage);
      }}
    >
      <div style={{
        display: 'flex', justifyContent: 'space-between', alignItems: 'baseline',
        padding: '4px 6px',
        background: 'var(--phos-fg)', color: 'var(--phos-bg-deep)',
        marginBottom: 12,
        textShadow: 'none',
      }}>
        <span className="h-bitmap-sm" style={{ letterSpacing: '0.1em' }}>{meta.dir}</span>
        <span style={{ fontSize: 11, opacity: 0.8 }}>{projects.length}</span>
      </div>
      <div className="fg-dim" style={{ fontSize: 11, marginBottom: 12, textTransform: 'uppercase' }}>
        // {meta.desc}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
        {projects.map(p => (
          <CardASCII key={p.slug} project={p} />
        ))}
        {projects.length === 0 && (
          <div className="fg-faint" style={{
            border: '1px dashed var(--phos-fg-faint)',
            padding: 24, textAlign: 'center', fontSize: 11,
          }}>(empty)</div>
        )}
      </div>
    </div>
  );
}

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
  const totalFeatures = projects.reduce((s, p) => {
    const counts = p.feature_counts ?? {};
    return s + Object.values(counts).reduce((a, n) => a + n, 0);
  }, 0);

  const byStage = (stage: Stage) =>
    projects.filter(p => p.stage === stage).sort(sortByInterest);

  const handleDrop = (slug: string, newStage: Stage) => {
    const project = projects.find(p => p.slug === slug);
    if (project && project.stage !== newStage) {
      mutation.mutate({ slug, stage: newStage });
    }
  };

  const now = new Date();
  const dateStr = now.toLocaleDateString('en-US', { weekday: 'short', day: '2-digit', month: 'short', year: 'numeric' }).toUpperCase();
  const timeStr = now.toLocaleTimeString('en-US', { hour12: false });

  if (isLoading) {
    return (
      <div style={{
        height: '100%', display: 'flex', alignItems: 'center', justifyContent: 'center',
        fontFamily: 'var(--font-mono)', color: 'var(--phos-fg-dim)',
      }}>
        Loading<span className="cursor-inline" />
      </div>
    );
  }

  return (
    <div style={{ padding: '18px 28px 24px', height: '100%', overflow: 'auto' }}>
      {/* Header bar */}
      <div style={{
        display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end',
        marginBottom: 16, paddingBottom: 8,
        borderBottom: '1px solid var(--phos-fg-faint)',
      }}>
        <div>
          <div className="h-bitmap-lg fg-bright" style={{ letterSpacing: '0.08em' }}>
            P E T B O A R D<span className="cursor-inline" />
          </div>
          <div className="fg-dim" style={{ fontSize: 11, marginTop: 2 }}>
            commonlisp6@homelab &middot; ~/code/services/petboard &middot; sse:open
          </div>
        </div>
        <div className="fg-dim" style={{ fontSize: 11, textAlign: 'right' }}>
          {projects.length} PROJECTS &middot; {totalFeatures} FEATURES<br/>
          {dateStr} &middot; {timeStr}
        </div>
      </div>

      {/* Three columns */}
      <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: 18 }}>
        {STAGES.map(stage => (
          <ColumnASCII
            key={stage}
            stage={stage}
            projects={byStage(stage)}
            onDrop={handleDrop}
          />
        ))}
      </div>

      {/* Status line */}
      <div style={{
        position: 'sticky', bottom: 0, marginTop: 22,
        background: 'var(--phos-bg-deep)',
        borderTop: '1px solid var(--phos-fg-faint)',
        padding: '8px 0',
        display: 'flex', justifyContent: 'space-between',
        fontSize: 11,
      }}>
        <span className="fg-dim">
          <Link to="/todos" style={{ color: 'inherit', textDecoration: 'none' }}>^T TODOS</Link>
          {' '}&nbsp; ^/ SEARCH &nbsp; ^Q QUIT
        </span>
        <span className="fg-dim">READY</span>
      </div>
    </div>
  );
}
