// CanvasUniverse renders the petboard "universe" — every project as a
// glowing thread on an infinite, pan-and-zoom canvas. Task #8 added the
// basic stage + threads + orbs + semantic zoom; task #9 adds:
//
//   - drag a project label vertically to set canvas_y (persisted)
//   - priority + status filter chips (top-left)
//   - time-window slider that fades features outside the window
//   - side drawer that opens when you click a feature orb
//
// Coordinate system:
//   - World coordinates are stable across zooms. X axis is time, with
//     1 world pixel ≈ BASE_PX_PER_DAY/86400 seconds at scale=1. Y axis
//     is project lane * LANE_HEIGHT, offset by HEADER_OFFSET.
//   - Stage scale + position transforms world → screen.
//
// Semantic zoom thresholds:
//   - scale < 0.4   → just threads + orbs, no labels
//   - 0.4 ≤ scale < 1.5 → project labels visible at start of each thread
//   - scale ≥ 1.5  → feature labels visible next to orbs

import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Stage, Layer, Line, Circle, Text, Group, Rect } from "react-konva";
import type Konva from "konva";
import { api } from "../api/client";
import type {
  Feature,
  Priority,
  Project,
  ProjectDetail,
  Status,
} from "../api/types";
import { formatDate, formatRelative } from "../lib/format";
import { exportUniverseSVG, downloadSVG } from "./svgExport";

// A high-priority project counts as "stale" once this many seconds
// have passed without any logged effort. The canvas draws a pulsing
// dashed ring around the label when this threshold is crossed.
const STALE_HIGH_PRIORITY_SECS = 14 * 86400; // 14 days

// ----- layout constants ---------------------------------------------------

const BASE_PX_PER_DAY = 8;
const LANE_HEIGHT = 90;
const HEADER_OFFSET = 80;
const ORB_RADIUS = 6;
const SECS_PER_DAY = 86400;

const STATUS_COLORS: Record<Status, string> = {
  backlog: "#a3a3a3",
  in_progress: "#fbbf24",
  done: "#34d399",
  dropped: "#525252",
};

// ----- types -------------------------------------------------------------

export interface UniverseData {
  projects: Project[];
  details?: Record<string, ProjectDetail>;
}

interface Props {
  data: UniverseData;
  width: number;
  height: number;
}

interface SelectedFeature {
  feature: Feature;
  projectName: string;
  projectColor: string;
}

const ALL_PRIORITIES: Priority[] = ["high", "medium", "low"];
const ALL_STATUSES: Status[] = ["backlog", "in_progress", "done", "dropped"];

// ----- main component ----------------------------------------------------

export default function CanvasUniverse({ data, width, height }: Props) {
  const stageRef = useRef<Konva.Stage>(null);
  const navigate = useNavigate();
  const queryClient = useQueryClient();

  // viewport transform
  const [scale, setScale] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });

  // current time tick
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now() / 1000), 60_000);
    return () => clearInterval(id);
  }, []);

  // Pulse phase for stale-project nudge — 0..1, ticks 30 times/sec
  // while the canvas is mounted. Cheap; React only re-renders when the
  // value crosses a threshold so this doesn't thrash.
  const [pulse, setPulse] = useState(0);
  useEffect(() => {
    let raf = 0;
    const start = performance.now();
    const tick = (t: number) => {
      // 1.5s pulse period
      setPulse(((t - start) / 1500) % 1);
      raf = requestAnimationFrame(tick);
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, []);

  // filters
  const [enabledPriorities, setEnabledPriorities] = useState<Set<Priority>>(
    new Set(ALL_PRIORITIES),
  );
  const [enabledStatuses, setEnabledStatuses] = useState<Set<Status>>(
    new Set(ALL_STATUSES),
  );

  // time window: how many days back from now to highlight (Infinity = all)
  const [windowDays, setWindowDays] = useState<number>(0); // 0 = all-time

  // side drawer
  const [selected, setSelected] = useState<SelectedFeature | null>(null);

  // mutation: persist canvas_y on drag end
  const updateY = useMutation({
    mutationFn: ({ slug, y }: { slug: string; y: number }) =>
      api.updateProject(slug, { canvas_y: y }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["projects"] });
    },
  });

  // ----- derived: time origin + lane assignment -------------------------

  const tOrigin = useMemo(() => {
    if (!data.projects.length) return now;
    return Math.min(...data.projects.map((p) => p.created_at));
  }, [data.projects, now]);

  const xForTime = (t: number) =>
    ((t - tOrigin) / SECS_PER_DAY) * BASE_PX_PER_DAY;

  // Visible projects (filtered by priority); preserve declared canvas_y
  // when set, otherwise auto-lay-out by created_at.
  const lanes = useMemo(() => {
    const filtered = data.projects.filter((p) =>
      enabledPriorities.has(p.priority),
    );
    const sorted = filtered
      .slice()
      .sort((a, b) => a.created_at - b.created_at);
    return sorted.map((p, i) => ({
      project: p,
      y: p.canvas_y ?? HEADER_OFFSET + i * LANE_HEIGHT,
    }));
  }, [data.projects, enabledPriorities]);

  // ----- pan + zoom handlers --------------------------------------------

  const handleWheel = (e: Konva.KonvaEventObject<WheelEvent>) => {
    e.evt.preventDefault();
    const stage = stageRef.current;
    if (!stage) return;
    const oldScale = scale;
    const pointer = stage.getPointerPosition();
    if (!pointer) return;
    const scaleBy = 1.06;
    const direction = e.evt.deltaY > 0 ? -1 : 1;
    const newScale = clamp(
      direction > 0 ? oldScale * scaleBy : oldScale / scaleBy,
      0.05,
      8,
    );
    const mousePointTo = {
      x: (pointer.x - offset.x) / oldScale,
      y: (pointer.y - offset.y) / oldScale,
    };
    setScale(newScale);
    setOffset({
      x: pointer.x - mousePointTo.x * newScale,
      y: pointer.y - mousePointTo.y * newScale,
    });
  };

  const handleStageDragEnd = (e: Konva.KonvaEventObject<DragEvent>) => {
    if (e.target === stageRef.current) {
      setOffset({ x: e.target.x(), y: e.target.y() });
    }
  };

  // fit-all
  const fitAll = () => {
    if (!data.projects.length) return;
    const xMin = 0;
    const xMax = xForTime(now) + 80;
    const yMin = HEADER_OFFSET - 40;
    const yMax = HEADER_OFFSET + Math.max(lanes.length, 1) * LANE_HEIGHT;
    const w = xMax - xMin;
    const h = yMax - yMin;
    const padding = 60;
    const sx = (width - padding * 2) / w;
    const sy = (height - padding * 2) / h;
    const newScale = clamp(Math.min(sx, sy), 0.05, 4);
    setScale(newScale);
    setOffset({
      x: padding - xMin * newScale,
      y: padding - yMin * newScale,
    });
  };

  // initial fit
  useEffect(() => {
    if (
      data.projects.length > 0 &&
      scale === 1 &&
      offset.x === 0 &&
      offset.y === 0
    ) {
      fitAll();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [data.projects.length, width, height]);

  // ----- keyboard shortcuts ---------------------------------------------
  // f       fit-all
  // +/=     zoom in
  // -       zoom out
  // 0       reset to 100%
  // escape  close side drawer
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      // Don't intercept when an input/textarea is focused
      const target = e.target as HTMLElement | null;
      if (target && (target.tagName === "INPUT" || target.tagName === "TEXTAREA" || target.isContentEditable)) {
        return;
      }
      switch (e.key) {
        case "f":
          fitAll();
          break;
        case "+":
        case "=":
          setScale((s) => clamp(s * 1.5, 0.05, 8));
          break;
        case "-":
          setScale((s) => clamp(s / 1.5, 0.05, 8));
          break;
        case "0":
          setScale(1);
          setOffset({ x: 0, y: 0 });
          break;
        case "Escape":
          if (selected) setSelected(null);
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected]);

  // ----- semantic zoom flags --------------------------------------------

  const showProjectLabels = scale >= 0.4;
  const showFeatureLabels = scale >= 1.5;

  // ----- stale-project detection ----------------------------------------

  // A high-priority project is "stale" if it has gone STALE_HIGH_PRIORITY_SECS
  // without any logged effort. We compute the latest effort timestamp
  // per project from the loaded detail bundles.
  const isStale = (project: Project): boolean => {
    if (project.priority !== "high") return false;
    const detail = data.details?.[project.slug];
    if (!detail) return false;
    let latest = 0;
    for (const e of detail.effort) {
      if (e.logged_at > latest) latest = e.logged_at;
    }
    if (latest === 0) return now - project.created_at > STALE_HIGH_PRIORITY_SECS;
    return now - latest > STALE_HIGH_PRIORITY_SECS;
  };

  // ----- time window opacity --------------------------------------------

  const featureOpacity = (f: Feature): number => {
    if (windowDays === 0) return 1;
    const cutoff = now - windowDays * SECS_PER_DAY;
    const t = f.completed_at ?? f.started_at ?? f.created_at;
    if (t < cutoff) return 0.18;
    return 1;
  };

  // ----- render ---------------------------------------------------------

  return (
    <div className="relative w-full h-full bg-neutral-950">
      <Stage
        ref={stageRef}
        width={width}
        height={height}
        draggable
        x={offset.x}
        y={offset.y}
        scaleX={scale}
        scaleY={scale}
        onWheel={handleWheel}
        onDragEnd={handleStageDragEnd}
      >
        {/* Background grid */}
        <Layer listening={false}>
          <BackgroundGrid
            width={width}
            height={height}
            scale={scale}
            offset={offset}
          />
        </Layer>

        {/* Threads + orbs */}
        <Layer>
          {lanes.map(({ project, y }) => (
            <ProjectThread
              key={project.id}
              project={project}
              y={y}
              now={now}
              xForTime={xForTime}
              detail={data.details?.[project.slug]}
              showLabel={showProjectLabels}
              showFeatureLabels={showFeatureLabels}
              scale={scale}
              enabledStatuses={enabledStatuses}
              featureOpacity={featureOpacity}
              stale={isStale(project)}
              pulse={pulse}
              onLabelClick={() => navigate(`/p/${project.slug}`)}
              onLabelDragEnd={(newY) =>
                updateY.mutate({ slug: project.slug, y: newY })
              }
              onOrbClick={(feature) =>
                setSelected({
                  feature,
                  projectName: project.name,
                  projectColor: project.color,
                })
              }
            />
          ))}

          {/* Now line */}
          <NowLine
            x={xForTime(now)}
            yTop={HEADER_OFFSET - 40}
            yBottom={HEADER_OFFSET + Math.max(lanes.length, 1) * LANE_HEIGHT}
          />
        </Layer>
      </Stage>

      {/* Top-left filter chips */}
      <FilterPanel
        priorities={enabledPriorities}
        onTogglePriority={(p) => toggleSet(setEnabledPriorities, p)}
        statuses={enabledStatuses}
        onToggleStatus={(s) => toggleSet(setEnabledStatuses, s)}
      />

      {/* Top-center time window slider */}
      <TimeWindowSlider value={windowDays} onChange={setWindowDays} />

      {/* Top-right zoom controls + export */}
      <div className="absolute top-3 right-3 flex gap-2">
        <button
          type="button"
          onClick={() => {
            const svg = exportUniverseSVG({
              projects: data.projects,
              details: data.details ?? {},
              width: Math.max(width, 800),
              height: Math.max(height, 400),
            });
            const stamp = new Date().toISOString().slice(0, 10);
            downloadSVG(svg, `petboard-universe-${stamp}.svg`);
          }}
          className="px-3 py-1 text-xs rounded border border-neutral-700 bg-neutral-900/80 backdrop-blur hover:bg-neutral-800"
          title="export current universe as SVG (for diary embeds)"
        >
          svg
        </button>
        <button
          type="button"
          onClick={fitAll}
          className="px-3 py-1 text-xs rounded border border-neutral-700 bg-neutral-900/80 backdrop-blur hover:bg-neutral-800"
          title="fit all to view"
        >
          fit
        </button>
        <button
          type="button"
          onClick={() => setScale((s) => clamp(s * 1.5, 0.05, 8))}
          className="px-3 py-1 text-xs rounded border border-neutral-700 bg-neutral-900/80 backdrop-blur hover:bg-neutral-800"
        >
          +
        </button>
        <button
          type="button"
          onClick={() => setScale((s) => clamp(s / 1.5, 0.05, 8))}
          className="px-3 py-1 text-xs rounded border border-neutral-700 bg-neutral-900/80 backdrop-blur hover:bg-neutral-800"
        >
          −
        </button>
      </div>

      {/* Bottom-right zoom indicator + shortcut hints */}
      <div className="absolute bottom-3 right-3 text-xs text-neutral-500 font-mono space-y-0.5 text-right">
        <div>
          zoom {scale.toFixed(2)}× · {lanes.length}/{data.projects.length} project
          {data.projects.length === 1 ? "" : "s"}
        </div>
        <div className="text-neutral-700">
          <kbd className="text-neutral-500">f</kbd> fit ·{" "}
          <kbd className="text-neutral-500">+/-</kbd> zoom ·{" "}
          <kbd className="text-neutral-500">esc</kbd> close
        </div>
      </div>

      {/* Side drawer */}
      {selected && (
        <FeatureDrawer
          selected={selected}
          onClose={() => setSelected(null)}
        />
      )}
    </div>
  );
}

// ----- subcomponents -----------------------------------------------------

function ProjectThread({
  project,
  y,
  now,
  xForTime,
  detail,
  showLabel,
  showFeatureLabels,
  scale,
  enabledStatuses,
  featureOpacity,
  stale,
  pulse,
  onLabelClick,
  onLabelDragEnd,
  onOrbClick,
}: {
  project: Project;
  y: number;
  now: number;
  xForTime: (t: number) => number;
  detail?: ProjectDetail;
  showLabel: boolean;
  showFeatureLabels: boolean;
  scale: number;
  enabledStatuses: Set<Status>;
  featureOpacity: (f: Feature) => number;
  stale: boolean;
  pulse: number;
  onLabelClick: () => void;
  onLabelDragEnd: (newY: number) => void;
  onOrbClick: (f: Feature) => void;
}) {
  const xStart = xForTime(project.created_at);
  const xEnd = xForTime(now);
  const labelScale = 1 / scale;

  // Drag state for the label so the thread + orbs follow it visually
  // before the mutation lands.
  const [dragY, setDragY] = useState<number | null>(null);
  const effectiveY = dragY ?? y;

  return (
    <Group>
      {/* Glow underlay */}
      <Line
        points={[xStart, effectiveY, xEnd, effectiveY]}
        stroke={project.color}
        strokeWidth={priorityStrokeWidth(project.priority) + 6}
        opacity={0.12}
        lineCap="round"
        listening={false}
      />
      {/* Main thread */}
      <Line
        points={[xStart, effectiveY, xEnd, effectiveY]}
        stroke={project.color}
        strokeWidth={priorityStrokeWidth(project.priority)}
        opacity={0.85}
        lineCap="round"
        listening={false}
      />

      {/* Project label — draggable on Y axis */}
      {showLabel && (
        <Group
          x={xStart}
          y={effectiveY - 22}
          scaleX={labelScale}
          scaleY={labelScale}
          draggable
          dragBoundFunc={(pos) => ({
            // freeze X (always anchor to thread start), allow Y to move
            x: xStart * scale + 0, // approximation — see onDragMove
            y: pos.y,
          })}
          onDragMove={(e) => {
            // The label group is positioned at (xStart, y - 22) in
            // world coords. Konva returns the new logical y on drag,
            // so we add 22 back to get the lane y.
            setDragY(e.target.y() + 22);
          }}
          onDragEnd={(e) => {
            const finalY = e.target.y() + 22;
            setDragY(null);
            // Snap reset of label position so on next render the
            // declarative y wins (we updated canvas_y on the server).
            e.target.y(y - 22);
            onLabelDragEnd(finalY);
          }}
          onClick={(e) => {
            // Only treat as click if not dragging (Konva fires click
            // even after a tiny drag).
            if (dragY === null) onLabelClick();
            e.cancelBubble = true;
          }}
          onTap={onLabelClick}
        >
          <Rect
            x={-4}
            y={-2}
            width={Math.max(project.name.length * 8 + 12, 60)}
            height={20}
            fill="#0a0a0a"
            stroke={project.color}
            strokeWidth={1}
            cornerRadius={3}
            opacity={0.92}
          />
          {/* Stale-project nudge: pulsing dashed outer ring around the
              label when this is a high-priority project that has not
              seen logged effort recently. */}
          {stale && (
            <Rect
              x={-7}
              y={-5}
              width={Math.max(project.name.length * 8 + 12, 60) + 6}
              height={26}
              fill="transparent"
              stroke="#fbbf24"
              strokeWidth={1.5}
              dash={[4, 3]}
              cornerRadius={5}
              opacity={0.4 + 0.55 * Math.abs(Math.sin(pulse * Math.PI))}
              listening={false}
            />
          )}
          <Text
            text={project.name}
            x={4}
            y={2}
            fontSize={13}
            fontFamily="ui-sans-serif, system-ui"
            fill="#e5e5e5"
          />
        </Group>
      )}

      {/* Feature orbs */}
      {(detail?.features ?? [])
        .filter((f) => enabledStatuses.has(f.status))
        .map((f) => (
          <FeatureOrb
            key={f.id}
            feature={f}
            y={effectiveY}
            xForTime={xForTime}
            showLabel={showFeatureLabels}
            labelScale={labelScale}
            opacity={featureOpacity(f)}
            onClick={() => onOrbClick(f)}
          />
        )) ?? null}
    </Group>
  );
}

function FeatureOrb({
  feature,
  y,
  xForTime,
  showLabel,
  labelScale,
  opacity,
  onClick,
}: {
  feature: Feature;
  y: number;
  xForTime: (t: number) => number;
  showLabel: boolean;
  labelScale: number;
  opacity: number;
  onClick: () => void;
}) {
  const xCreated = xForTime(feature.created_at);
  const xCompleted =
    feature.completed_at != null ? xForTime(feature.completed_at) : null;
  const color = STATUS_COLORS[feature.status];
  const baseOpacity =
    feature.status === "dropped" ? 0.35 * opacity : 0.95 * opacity;

  return (
    <Group onClick={onClick} onTap={onClick}>
      {xCompleted != null && (
        <Line
          points={[xCreated, y, xCompleted, y]}
          stroke={color}
          strokeWidth={1.5}
          opacity={0.5 * opacity}
          listening={false}
        />
      )}
      <Circle
        x={xCreated}
        y={y}
        radius={ORB_RADIUS}
        stroke={color}
        strokeWidth={1.5}
        fill={feature.status === "done" ? color : "transparent"}
        opacity={baseOpacity}
      />
      {xCompleted != null && (
        <Circle
          x={xCompleted}
          y={y}
          radius={ORB_RADIUS}
          fill={color}
          opacity={baseOpacity}
        />
      )}
      {showLabel && (
        <Group
          x={xCreated + 10}
          y={y - 6}
          scaleX={labelScale}
          scaleY={labelScale}
          listening={false}
        >
          <Text
            text={feature.title}
            fontSize={11}
            fontFamily="ui-sans-serif, system-ui"
            fill="#a3a3a3"
            opacity={opacity}
          />
        </Group>
      )}
    </Group>
  );
}

function NowLine({
  x,
  yTop,
  yBottom,
}: {
  x: number;
  yTop: number;
  yBottom: number;
}) {
  return (
    <Group listening={false}>
      <Line
        points={[x, yTop, x, yBottom]}
        stroke="#7aa2f7"
        strokeWidth={1.2}
        opacity={0.6}
        dash={[4, 4]}
      />
      <Circle x={x} y={yTop} radius={3} fill="#7aa2f7" opacity={0.8} />
    </Group>
  );
}

function BackgroundGrid({
  width,
  height,
  scale,
  offset,
}: {
  width: number;
  height: number;
  scale: number;
  offset: { x: number; y: number };
}) {
  const visibleLeft = -offset.x / scale;
  const visibleTop = -offset.y / scale;
  const visibleRight = visibleLeft + width / scale;
  const visibleBottom = visibleTop + height / scale;
  const desiredScreenStep = 80;
  const worldStep = niceStep(desiredScreenStep / scale);

  const lines: JSX.Element[] = [];
  let i = 0;
  for (
    let x = Math.floor(visibleLeft / worldStep) * worldStep;
    x < visibleRight;
    x += worldStep, i++
  ) {
    lines.push(
      <Line
        key={`v${i}`}
        points={[x, visibleTop, x, visibleBottom]}
        stroke="#262626"
        strokeWidth={1 / scale}
      />,
    );
  }
  let j = 0;
  for (
    let y = Math.floor(visibleTop / worldStep) * worldStep;
    y < visibleBottom;
    y += worldStep, j++
  ) {
    lines.push(
      <Line
        key={`h${j}`}
        points={[visibleLeft, y, visibleRight, y]}
        stroke="#262626"
        strokeWidth={1 / scale}
      />,
    );
  }
  return <>{lines}</>;
}

function FilterPanel({
  priorities,
  onTogglePriority,
  statuses,
  onToggleStatus,
}: {
  priorities: Set<Priority>;
  onTogglePriority: (p: Priority) => void;
  statuses: Set<Status>;
  onToggleStatus: (s: Status) => void;
}) {
  return (
    <div className="absolute top-3 left-3 flex flex-col gap-2 text-xs">
      <div className="flex gap-1.5 items-center">
        <span className="text-neutral-500 mr-1">priority:</span>
        {ALL_PRIORITIES.map((p) => (
          <Chip
            key={p}
            label={p}
            active={priorities.has(p)}
            onClick={() => onTogglePriority(p)}
            colorClass={
              p === "high"
                ? "border-red-500/40 text-red-300"
                : p === "medium"
                  ? "border-amber-500/40 text-amber-300"
                  : "border-sky-500/40 text-sky-300"
            }
          />
        ))}
      </div>
      <div className="flex gap-1.5 items-center">
        <span className="text-neutral-500 mr-1">status:</span>
        {ALL_STATUSES.map((s) => (
          <Chip
            key={s}
            label={s.replace("_", " ")}
            active={statuses.has(s)}
            onClick={() => onToggleStatus(s)}
            colorClass="border-neutral-700 text-neutral-300"
          />
        ))}
      </div>
    </div>
  );
}

function Chip({
  label,
  active,
  onClick,
  colorClass,
}: {
  label: string;
  active: boolean;
  onClick: () => void;
  colorClass: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-2 py-0.5 rounded border ${colorClass} backdrop-blur ${
        active ? "bg-neutral-900/80" : "bg-neutral-950/80 opacity-40"
      }`}
    >
      {label}
    </button>
  );
}

function TimeWindowSlider({
  value,
  onChange,
}: {
  value: number;
  onChange: (v: number) => void;
}) {
  const labels = ["all", "1y", "6mo", "3mo", "1mo", "1w"];
  const values = [0, 365, 180, 90, 30, 7];
  return (
    <div className="absolute top-3 left-1/2 -translate-x-1/2 text-xs flex items-center gap-2 bg-neutral-900/80 backdrop-blur border border-neutral-800 rounded px-3 py-1">
      <span className="text-neutral-500">window:</span>
      {labels.map((label, i) => (
        <button
          key={label}
          type="button"
          onClick={() => onChange(values[i])}
          className={`px-1.5 py-0.5 rounded ${
            value === values[i]
              ? "bg-neutral-700 text-neutral-100"
              : "text-neutral-400 hover:text-neutral-200"
          }`}
        >
          {label}
        </button>
      ))}
    </div>
  );
}

function FeatureDrawer({
  selected,
  onClose,
}: {
  selected: SelectedFeature;
  onClose: () => void;
}) {
  const { feature, projectName, projectColor } = selected;
  return (
    <aside className="absolute top-0 right-0 h-full w-80 bg-neutral-900 border-l border-neutral-800 p-5 overflow-y-auto shadow-2xl">
      <div className="flex items-center justify-between mb-4">
        <div className="flex items-center gap-2">
          <span
            className="h-3 w-3 rounded-full"
            style={{ backgroundColor: projectColor }}
          />
          <span className="text-sm text-neutral-400">{projectName}</span>
        </div>
        <button
          type="button"
          onClick={onClose}
          className="text-neutral-500 hover:text-neutral-200 text-lg leading-none"
          aria-label="close"
        >
          ×
        </button>
      </div>
      <h2 className="text-lg font-medium leading-tight">{feature.title}</h2>
      <span
        className="inline-block mt-2 px-2 py-0.5 text-xs uppercase tracking-wide rounded border"
        style={{
          color: STATUS_COLORS[feature.status],
          borderColor: STATUS_COLORS[feature.status] + "60",
        }}
      >
        {feature.status.replace("_", " ")}
      </span>
      {feature.description && (
        <p className="mt-4 text-sm text-neutral-300 leading-relaxed">
          {feature.description}
        </p>
      )}
      <dl className="mt-6 space-y-2 text-xs">
        <Row label="created" value={`${formatDate(feature.created_at)} (${formatRelative(feature.created_at)})`} />
        {feature.started_at != null && (
          <Row label="started" value={`${formatDate(feature.started_at)} (${formatRelative(feature.started_at)})`} />
        )}
        {feature.completed_at != null && (
          <Row label="completed" value={`${formatDate(feature.completed_at)} (${formatRelative(feature.completed_at)})`} />
        )}
        {feature.dropped_at != null && (
          <Row label="dropped" value={`${formatDate(feature.dropped_at)} (${formatRelative(feature.dropped_at)})`} />
        )}
      </dl>
    </aside>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-3">
      <dt className="text-neutral-500">{label}</dt>
      <dd className="text-neutral-300 text-right">{value}</dd>
    </div>
  );
}

// ----- helpers -----------------------------------------------------------

function clamp(v: number, lo: number, hi: number) {
  return Math.max(lo, Math.min(hi, v));
}

function priorityStrokeWidth(priority: string): number {
  if (priority === "high") return 4;
  if (priority === "low") return 1.5;
  return 2.5;
}

function niceStep(target: number): number {
  if (target <= 0) return 1;
  const exp = Math.floor(Math.log10(target));
  const base = Math.pow(10, exp);
  const norm = target / base;
  let nice;
  if (norm < 1.5) nice = 1;
  else if (norm < 3) nice = 2;
  else if (norm < 7) nice = 5;
  else nice = 10;
  return nice * base;
}

function toggleSet<T>(setter: (fn: (prev: Set<T>) => Set<T>) => void, item: T) {
  setter((prev) => {
    const next = new Set(prev);
    if (next.has(item)) next.delete(item);
    else next.add(item);
    return next;
  });
}
