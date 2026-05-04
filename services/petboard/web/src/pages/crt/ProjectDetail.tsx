import { useState, useRef, useEffect, useMemo } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../../api/client";
import type { Feature, Status, ProjectDetail as ProjectDetailType, Interest } from "../../api/types";
import { formatHours, formatDate, formatRelative } from "../../lib/format";
import * as audio from "../../lib/audio";
import KernelPanic from "../../components/KernelPanic";

const STATUS_NEXT: Record<Status, Status> = {
  backlog: "in_progress",
  in_progress: "done",
  done: "backlog",
  dropped: "backlog",
};

const STAGE_META: Record<string, { label: string; dir: string }> = {
  idea:      { label: "IDEA",      dir: "/idea" },
  live:      { label: "LIVE",      dir: "/live" },
  completed: { label: "COMPLETED", dir: "/completed" },
};

const INTEREST_META: Record<Interest, { label: string; short: string }> = {
  excited: { label: "EXCITED", short: "EXC" },
  meh:     { label: "MEH",     short: "MEH" },
  bored:   { label: "BORED",   short: "BOR" },
};

function daysSince(unixSeconds: number | undefined): number | null {
  if (!unixSeconds) return null;
  const ms = Date.now() - unixSeconds * 1000;
  return Math.max(0, Math.floor(ms / 86400000));
}

function progressBar(features: Feature[], width = 12): string {
  if (!features || features.length === 0) return '[' + '\u2591'.repeat(width) + ']';
  const done = features.filter(f => f.status === 'done').length;
  const total = features.filter(f => f.status !== 'dropped').length;
  if (total === 0) return '[' + '\u2591'.repeat(width) + ']';
  const filled = Math.round((done / total) * width);
  return '[' + '\u2588'.repeat(filled) + '\u2591'.repeat(width - filled) + ']';
}

function SectionHeader({ title, hint }: { title: string; hint?: string }) {
  return (
    <div style={{
      display: 'flex', justifyContent: 'space-between', alignItems: 'baseline',
      borderBottom: '1px solid var(--phos-fg-dim)', paddingBottom: 4,
    }}>
      <span className="h-bitmap-sm fg-bright" style={{ letterSpacing: '0.05em' }}>{title}</span>
      {hint && <span className="fg-faint" style={{ fontSize: 11 }}>{hint}</span>}
    </div>
  );
}

// ─── Handoff Modal (dot-matrix printer) ─────────────────────────

function HandoffModal({ project, onClose }: { project: ProjectDetailType; onClose: () => void }) {
  const allIds = (project.features ?? []).map(f => f.id);
  const [included, setIncluded] = useState<Set<number>>(new Set(allIds));
  const [printing, setPrinting] = useState(false);
  const [printed, setPrinted] = useState<string[]>([]);
  const [copyHint, setCopyHint] = useState('CLICK PAPER TO COPY');
  const tokenRef = useRef(0);

  const markdown = useMemo(() => {
    const lines: string[] = [];
    lines.push(`# ${project.name}`);
    lines.push('');
    lines.push(`**Stage:** ${project.stage}`);
    lines.push('');
    lines.push('## Problem');
    lines.push('');
    lines.push(project.problem);
    lines.push('');
    if (project.description) {
      lines.push('## Description');
      lines.push('');
      lines.push(project.description);
      lines.push('');
    }
    const picked = (project.features ?? []).filter(f => included.has(f.id));
    if (picked.length) {
      lines.push('## Features');
      lines.push('');
      for (const f of picked) {
        const mark = f.status === 'done' ? '[x]' : f.status === 'dropped' ? '[~]' : '[ ]';
        lines.push(`- ${mark} **${f.title}**  _(${f.status})_`);
      }
      lines.push('');
    }
    return lines.join('\n');
  }, [project, included]);

  function toggle(id: number) {
    setIncluded(prev => {
      const n = new Set(prev);
      if (n.has(id)) n.delete(id); else n.add(id);
      return n;
    });
  }

  function startPrint() {
    const myToken = ++tokenRef.current;
    setPrinted([]);
    setPrinting(true);
    const lines = markdown.split('\n');
    let i = 0;
    function tick() {
      if (myToken !== tokenRef.current) return;
      if (i >= lines.length) { setPrinting(false); return; }
      setPrinted(prev => [...prev, lines[i]]);
      audio.click(0.06);
      i++;
      setTimeout(tick, 28 + Math.random() * 22);
    }
    tick();
  }

  useEffect(() => {
    startPrint();
    audio.beep(1200, 0.05, 0.06);
    return () => { tokenRef.current++; };
  }, [Array.from(included).sort().join(',')]);

  function copy() {
    navigator.clipboard?.writeText(markdown);
    setCopyHint('\u2713 COPIED');
    audio.beep(2200, 0.08, 0.06);
    setTimeout(() => setCopyHint('CLICK PAPER TO COPY'), 1400);
  }

  useEffect(() => {
    const k = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose(); };
    window.addEventListener('keydown', k);
    return () => window.removeEventListener('keydown', k);
  }, [onClose]);

  return (
    <div className="modal-backdrop" onClick={onClose}>
      <div onClick={e => e.stopPropagation()} style={{
        display: 'grid',
        gridTemplateColumns: '320px 560px',
        gap: 0,
        maxHeight: '88vh',
      }}>
        {/* LEFT: feature checklist */}
        <div style={{
          background: 'var(--phos-bg-deep)',
          border: '1px solid var(--phos-fg-dim)',
          borderRight: 'none',
          padding: '14px 18px',
          fontFamily: 'var(--font-mono)',
          fontSize: 12,
          maxHeight: '88vh',
          overflow: 'auto',
        }}>
          <div className="h-bitmap-md fg-bright" style={{ marginBottom: 6 }}>HANDOFF.MD</div>
          <div className="fg-dim" style={{ marginBottom: 14, fontSize: 11 }}>
            EPSON&middot;FX-80 &middot; 9-PIN &middot; DRAFT
          </div>
          <div className="fg-dim" style={{ marginBottom: 6, fontSize: 11, textTransform: 'uppercase' }}>Project</div>
          <div className="fg-bright tt-bitmap" style={{ fontSize: 22, marginBottom: 14 }}>{project.name}</div>

          <div className="fg-dim" style={{ marginBottom: 8, fontSize: 11, textTransform: 'uppercase' }}>
            Features ({included.size}/{(project.features ?? []).length} selected)
          </div>
          {(project.features ?? []).length === 0 && (
            <div className="fg-faint" style={{ fontStyle: 'italic' }}>(no features yet)</div>
          )}
          {(project.features ?? []).map(f => {
            const on = included.has(f.id);
            return (
              <div key={f.id} data-click="1" style={{
                display: 'flex', alignItems: 'flex-start', gap: 8,
                cursor: 'pointer', padding: '4px 0',
                color: on ? 'var(--phos-fg)' : 'var(--phos-fg-faint)',
              }} onClick={() => toggle(f.id)}>
                <span className="tt-mono" style={{ flex: '0 0 auto' }}>
                  {on ? '[X]' : '[ ]'}
                </span>
                <span style={{ flex: 1, lineHeight: 1.3 }}>{f.title}</span>
                <span className="fg-faint" style={{ fontSize: 10, textTransform: 'uppercase' }}>{f.status}</span>
              </div>
            );
          })}

          <div style={{ marginTop: 18, display: 'flex', gap: 8 }}>
            <button className="btn" data-click="1" onClick={copy}>COPY</button>
            <button className="btn" data-click="1" onClick={startPrint} disabled={printing}>RE-PRINT</button>
            <button className="btn btn-danger" data-click="1" onClick={onClose}>ESC</button>
          </div>
        </div>

        {/* RIGHT: dot-matrix paper */}
        <div style={{
          background: '#f5efd6',
          color: '#1a1a1a',
          padding: '24px 28px',
          fontFamily: 'var(--font-mono)',
          fontSize: 12.5,
          lineHeight: 1.45,
          maxHeight: '88vh',
          overflow: 'auto',
          textShadow: 'none',
          cursor: 'pointer',
          boxShadow: 'inset 0 0 60px rgba(120, 100, 60, 0.25)',
          backgroundImage: 'repeating-linear-gradient(to bottom, rgba(0,0,0,0) 0, rgba(0,0,0,0) 23px, rgba(0,0,0,0.04) 24px)',
        }} onClick={copy}>
          {/* Tractor-feed perforation strip */}
          <div style={{
            display: 'flex', justifyContent: 'space-between',
            color: '#a08868', fontSize: 10, marginBottom: 16, letterSpacing: '0.4em',
          }}>
            <span>{'\u2022 '.repeat(8)}</span>
            <span>PETBOARD/HANDOFF</span>
            <span>{' \u2022'.repeat(8)}</span>
          </div>
          <pre style={{ margin: 0, whiteSpace: 'pre-wrap', fontFamily: 'inherit', fontSize: 'inherit' }}>
{printed.join('\n')}{printing && '\u258C'}
          </pre>
          <div style={{
            marginTop: 22, fontSize: 10, color: '#a08868',
            borderTop: '1px dashed #c4a878', paddingTop: 8, textAlign: 'center',
            letterSpacing: '0.2em',
          }}>
            {printing ? 'PRINTING\u2026' : copyHint}
          </div>
        </div>
      </div>
    </div>
  );
}

// ─── Main detail page ───────────────────────────────────────────

export default function ProjectDetail() {
  const { slug = "" } = useParams();
  const queryClient = useQueryClient();

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
    mutationFn: (body: Parameters<typeof api.updateProject>[1]) => api.updateProject(slug, body),
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
    mutationFn: (body: Parameters<typeof api.logEffort>[1]) => api.logEffort(slug, body),
    onSuccess: invalidate,
  });

  const [newFeatureTitle, setNewFeatureTitle] = useState("");
  const [effortMinutes, setEffortMinutes] = useState("");
  const [effortNote, setEffortNote] = useState("");
  const [editingProblem, setEditingProblem] = useState(false);
  const [editingName, setEditingName] = useState(false);
  const [showHandoff, setShowHandoff] = useState(false);
  const [showPanic, setShowPanic] = useState(false);

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

  if (error) {
    return (
      <div style={{ padding: '20px 32px', fontFamily: 'var(--font-mono)' }}>
        <Link to="/" className="fg-dim" style={{ fontSize: 11, textDecoration: 'none' }}>ESC BACK</Link>
        <div style={{ marginTop: 16, color: 'var(--danger)' }}>
          Failed to load project: {(error as Error).message}
        </div>
      </div>
    );
  }

  if (!data) return null;

  const features = data.features ?? [];
  const effort = data.effort ?? [];
  const stage = STAGE_META[data.stage] ?? { label: data.stage, dir: '/' + data.stage };
  const interest = INTEREST_META[data.interest];
  const totalFeat = features.length;
  const doneFeat = features.filter(f => f.status === 'done').length;
  const startedFeat = features.filter(f => f.status === 'in_progress').length;
  const idle = daysSince(effort.length > 0 ? Math.max(...effort.map(e => e.logged_at)) : undefined);

  return (
    <div style={{
      position: 'absolute', inset: 0,
      padding: '20px 32px',
      overflow: 'auto',
      fontFamily: 'var(--font-mono)',
      fontSize: 13,
    }}>
      {/* Status line */}
      <div style={{
        display: 'flex', justifyContent: 'space-between',
        marginBottom: 18, paddingBottom: 6,
        borderBottom: '1px solid var(--phos-fg-faint)',
        fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.1em',
      }}>
        <span className="fg-dim">
          <Link to="/" style={{ color: 'inherit', textDecoration: 'none' }}>PETBOARD</Link>
          {' > '}{stage.dir} {'> '}{data.slug}
        </span>
        <span className="fg-dim">^Q QUIT &nbsp; ^H HANDOFF &nbsp; ^W WORK &nbsp; ESC BACK</span>
      </div>

      {/* Title block */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-end', marginBottom: 22, flexWrap: 'wrap', gap: 12 }}>
        <div>
          <div className="fg-dim" style={{ fontSize: 11, marginBottom: 4 }}>
            {data.id} &middot; {stage.label} &middot; PRIORITY {data.priority.toUpperCase()}
          </div>
          {editingName ? (
            <InlineEdit
              initial={data.name}
              onSave={(v) => {
                if (v && v !== data.name) updateProject.mutate({ name: v });
                setEditingName(false);
              }}
              onCancel={() => setEditingName(false)}
            />
          ) : (
            <div
              className="h-bitmap-xl fg-bright"
              onClick={() => setEditingName(true)}
              style={{ cursor: 'text' }}
              title="click to rename"
            >
              {data.name}
            </div>
          )}
        </div>
        <div style={{ display: 'flex', gap: 10 }}>
          <button className="btn" data-click="1" onClick={() => setShowHandoff(true)}>[H] HANDOFF</button>
          <button className="btn btn-danger" data-click="1" onClick={() => setShowPanic(true)}>[W] WORK ON IT</button>
          <Link to="/" className="btn" style={{ textDecoration: 'none' }}>[ESC]</Link>
        </div>
      </div>

      {/* Stat row */}
      <div style={{
        display: 'grid', gridTemplateColumns: 'repeat(5, 1fr)', gap: 14, marginBottom: 24,
      }}>
        {[
          { l: 'EFFORT', v: formatHours(data.total_minutes), d: data.total_minutes ? `${(data.total_minutes / 60).toFixed(1)} hours` : '-' },
          { l: 'CREATED', v: formatDate(data.created_at), d: `${daysSince(data.created_at)}d ago` },
          { l: 'LAST TOUCH', v: effort.length > 0 ? formatDate(Math.max(...effort.map(e => e.logged_at))) : '-', d: idle != null ? `${idle}d ago` : 'never' },
          { l: 'FEATURES', v: `${doneFeat}/${totalFeat}`, d: `${startedFeat} in flight` },
          { l: 'INTEREST', v: interest.label, d: interest.short },
        ].map(s => (
          <div key={s.l} style={{
            border: '1px solid var(--phos-fg-faint)',
            padding: '10px 12px',
          }}>
            <div className="fg-faint" style={{ fontSize: 10, textTransform: 'uppercase', letterSpacing: '0.1em' }}>{s.l}</div>
            <div className="h-bitmap-md fg-bright" style={{ margin: '4px 0' }}>{s.v}</div>
            <div className="fg-dim" style={{ fontSize: 11 }}>{s.d}</div>
          </div>
        ))}
      </div>

      {/* Two-pane: problem + features */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 24 }}>
        {/* Problem + Description */}
        <div>
          <SectionHeader title="PROBLEM" hint="why this exists" />
          {editingProblem ? (
            <InlineEdit
              multiline
              initial={data.problem}
              onSave={(v) => {
                if (v.trim() && v !== data.problem) updateProject.mutate({ problem: v });
                setEditingProblem(false);
              }}
              onCancel={() => setEditingProblem(false)}
            />
          ) : (
            <p className="fg" style={{ marginTop: 10, lineHeight: 1.55, cursor: 'text' }}
               onClick={() => setEditingProblem(true)} title="click to edit">
              {data.problem}
            </p>
          )}
          <div style={{ height: 18 }} />
          {data.description && (
            <>
              <SectionHeader title="DESCRIPTION" hint="what it is" />
              <p className="fg-dim" style={{ marginTop: 10, lineHeight: 1.55 }}>
                {data.description}
              </p>
            </>
          )}

          {/* Add feature */}
          <div style={{ marginTop: 24 }}>
            <SectionHeader title="ADD FEATURE" />
            <form onSubmit={(e) => {
              e.preventDefault();
              if (!newFeatureTitle.trim()) return;
              createFeature.mutate(newFeatureTitle.trim(), {
                onSuccess: () => setNewFeatureTitle(""),
              });
            }} style={{ display: 'flex', gap: 8, marginTop: 10 }}>
              <input
                type="text"
                value={newFeatureTitle}
                onChange={(e) => setNewFeatureTitle(e.target.value)}
                placeholder="what needs to happen?"
                className="crt-input"
                style={{ flex: 1 }}
              />
              <button type="submit" className="btn" disabled={!newFeatureTitle.trim() || createFeature.isPending}>
                ADD
              </button>
            </form>
          </div>

          {/* Log effort */}
          <div style={{ marginTop: 24 }}>
            <SectionHeader title="LOG EFFORT" />
            <form onSubmit={(e) => {
              e.preventDefault();
              const minutes = parseInt(effortMinutes, 10);
              if (!minutes || minutes <= 0) return;
              logEffort.mutate(
                { minutes, note: effortNote.trim() || undefined },
                { onSuccess: () => { setEffortMinutes(""); setEffortNote(""); } },
              );
            }} style={{ display: 'flex', gap: 8, marginTop: 10 }}>
              <input
                type="number"
                value={effortMinutes}
                onChange={(e) => setEffortMinutes(e.target.value)}
                placeholder="min"
                min="1"
                className="crt-input"
                style={{ width: 70 }}
              />
              <input
                type="text"
                value={effortNote}
                onChange={(e) => setEffortNote(e.target.value)}
                placeholder="what did you work on?"
                className="crt-input"
                style={{ flex: 1 }}
              />
              <button type="submit" className="btn" disabled={!effortMinutes || logEffort.isPending}>
                LOG
              </button>
            </form>
            {effort.length > 0 && (
              <div style={{ marginTop: 12 }}>
                {effort
                  .slice()
                  .sort((a, b) => b.logged_at - a.logged_at)
                  .slice(0, 8)
                  .map(e => (
                    <div key={e.id} className="fg-faint" style={{ fontSize: 11, padding: '3px 0', borderBottom: '1px dotted var(--phos-fg-faint)' }}>
                      <span>{formatRelative(e.logged_at)}</span>{' '}
                      <span className="fg-dim">{e.minutes}m</span>
                      {e.note && <span className="fg-dim"> — {e.note}</span>}
                    </div>
                  ))}
              </div>
            )}
          </div>
        </div>

        {/* Features */}
        <div>
          <SectionHeader title="FEATURES" hint={`${progressBar(features)} ${doneFeat}/${totalFeat}`} />
          <div style={{ marginTop: 10 }}>
            {features.length === 0 && (
              <div className="fg-faint" style={{ fontStyle: 'italic' }}>(no features logged yet)</div>
            )}
            {features.map((f, i) => {
              const mark = f.status === 'done' ? '[\u2713]'
                         : f.status === 'in_progress' ? '[*]'
                         : f.status === 'dropped' ? '[\u2717]'
                         : '[ ]';
              const cls = f.status === 'done' ? 'fg-dim'
                        : f.status === 'in_progress' ? 'fg-bright'
                        : f.status === 'dropped' ? 'fg-faint'
                        : 'fg';
              return (
                <div key={f.id} className={cls} style={{
                  display: 'flex', gap: 10, padding: '5px 0',
                  borderBottom: i < features.length - 1 ? '1px dotted var(--phos-fg-faint)' : 'none',
                  alignItems: 'flex-start',
                }}>
                  <button
                    className="tt-mono"
                    style={{
                      flex: '0 0 28px', background: 'none', border: 'none', color: 'inherit',
                      cursor: 'pointer', padding: 0, fontFamily: 'inherit', fontSize: 'inherit',
                      textShadow: 'inherit',
                    }}
                    onClick={() => updateFeature.mutate({ id: f.id, body: { status: STATUS_NEXT[f.status] } })}
                    title={`move to ${STATUS_NEXT[f.status]}`}
                  >
                    {mark}
                  </button>
                  <span style={{ flex: '0 0 28px' }} className="fg-faint">{String(f.id).padStart(2, '0')}</span>
                  <span style={{
                    flex: 1,
                    textDecoration: f.status === 'dropped' ? 'line-through' : 'none',
                  }}>{f.title}</span>
                  <span className="fg-faint" style={{ fontSize: 10, textTransform: 'uppercase' }}>{f.status}</span>
                  <button
                    style={{
                      background: 'none', border: 'none', color: 'var(--danger)',
                      cursor: 'pointer', padding: 0, fontFamily: 'var(--font-mono)', fontSize: 11,
                      opacity: 0.5, textShadow: 'none',
                    }}
                    onClick={() => { if (confirm(`Delete "${f.title}"?`)) deleteFeature.mutate(f.id); }}
                    title="delete"
                  >
                    x
                  </button>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <div style={{ height: 30 }} />

      {showHandoff && <HandoffModal project={data} onClose={() => setShowHandoff(false)} />}
      {showPanic && <KernelPanic projectName={data.name} onDismiss={() => setShowPanic(false)} />}
    </div>
  );
}

// ─── Inline editor ──────────────────────────────────────────────

function InlineEdit({
  initial, onSave, onCancel, multiline = false
}: {
  initial: string;
  onSave: (v: string) => void;
  onCancel: () => void;
  multiline?: boolean;
}) {
  const [value, setValue] = useState(initial);
  const ref = useRef<HTMLInputElement | HTMLTextAreaElement>(null);

  useEffect(() => {
    ref.current?.focus();
    if (ref.current && 'select' in ref.current) {
      (ref.current as HTMLInputElement).select?.();
    }
  }, []);

  const handleKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onCancel(); }
    if (e.key === 'Enter' && !multiline) { e.preventDefault(); onSave(value); }
    if (e.key === 'Enter' && multiline && (e.metaKey || e.ctrlKey)) { e.preventDefault(); onSave(value); }
  };

  if (multiline) {
    return (
      <textarea
        ref={ref as React.RefObject<HTMLTextAreaElement>}
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onBlur={() => onSave(value)}
        onKeyDown={handleKey}
        className="crt-input"
        style={{ width: '100%', minHeight: 120, marginTop: 10, lineHeight: 1.55, resize: 'vertical' }}
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
      className="crt-input h-bitmap-xl fg-bright"
      style={{ width: '100%' }}
    />
  );
}
