// SVG export of the canvas universe.
//
// Konva can rasterize a Stage to PNG via toDataURL but it cannot emit
// SVG natively. Rather than pull in a heavy konva→svg shim, we walk the
// same data the React canvas walks and emit equivalent SVG. The output
// is intentionally self-contained (single <svg> element with inline
// styles) so it can be pasted directly into a Hugo markdown file in the
// diary.

import type { Project, ProjectDetail, Status } from "../api/types";

const SECS_PER_DAY = 86400;
const BASE_PX_PER_DAY = 8;
const LANE_HEIGHT = 90;
const HEADER_OFFSET = 80;
const ORB_RADIUS = 6;

const STATUS_COLORS: Record<Status, string> = {
  backlog: "#a3a3a3",
  in_progress: "#fbbf24",
  done: "#34d399",
  dropped: "#525252",
};

function priorityStrokeWidth(priority: string): number {
  if (priority === "high") return 4;
  if (priority === "low") return 1.5;
  return 2.5;
}

function escapeXML(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

interface ExportOpts {
  projects: Project[];
  details: Record<string, ProjectDetail>;
  width?: number;
  height?: number;
}

/**
 * Render the universe to a self-contained SVG string. The viewport is
 * computed from the data — we always fit-all rather than capturing the
 * user's current pan/zoom, because the export is meant for the diary
 * (where "the whole universe at this moment" is the useful artifact).
 */
export function exportUniverseSVG({ projects, details, width = 1200, height = 600 }: ExportOpts): string {
  const now = Date.now() / 1000;
  if (projects.length === 0) {
    return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}"><rect width="100%" height="100%" fill="#0a0a0a"/><text x="50%" y="50%" fill="#737373" font-family="ui-sans-serif,system-ui" font-size="16" text-anchor="middle">empty universe</text></svg>`;
  }

  const tOrigin = Math.min(...projects.map((p) => p.created_at));
  const xForTime = (t: number) => ((t - tOrigin) / SECS_PER_DAY) * BASE_PX_PER_DAY;

  // Auto-lay-out: sort by created_at, lanes top-down. Honor canvas_y if set.
  const sorted = projects.slice().sort((a, b) => a.created_at - b.created_at);
  const lanes = sorted.map((p, i) => ({
    project: p,
    y: p.canvas_y ?? HEADER_OFFSET + i * LANE_HEIGHT,
  }));

  // World bounds
  const xMin = 0;
  const xMax = xForTime(now) + 80;
  const yMin = HEADER_OFFSET - 40;
  const yMax = HEADER_OFFSET + lanes.length * LANE_HEIGHT;
  const w = xMax - xMin;
  const h = yMax - yMin;

  // Fit world into the requested viewport with padding.
  const padding = 40;
  const sx = (width - padding * 2) / w;
  const sy = (height - padding * 2) / h;
  const scale = Math.max(0.05, Math.min(sx, sy));
  const tx = padding - xMin * scale;
  const ty = padding - yMin * scale;

  const parts: string[] = [];
  parts.push(
    `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" font-family="ui-sans-serif,system-ui">`,
  );
  // Background
  parts.push(`<rect width="100%" height="100%" fill="#0a0a0a"/>`);

  // World group with the fit transform
  parts.push(`<g transform="translate(${tx},${ty}) scale(${scale})">`);

  // Faint vertical day grid (one line per week)
  for (let x = xMin; x < xMax; x += BASE_PX_PER_DAY * 7) {
    parts.push(
      `<line x1="${x}" y1="${yMin}" x2="${x}" y2="${yMax}" stroke="#1f1f1f" stroke-width="${1 / scale}"/>`,
    );
  }

  // Project threads + orbs
  for (const { project, y } of lanes) {
    const xStart = xForTime(project.created_at);
    const xEnd = xForTime(now);
    const sw = priorityStrokeWidth(project.priority);

    // Glow underlay
    parts.push(
      `<line x1="${xStart}" y1="${y}" x2="${xEnd}" y2="${y}" stroke="${project.color}" stroke-width="${sw + 6}" stroke-opacity="0.12" stroke-linecap="round"/>`,
    );
    // Main thread
    parts.push(
      `<line x1="${xStart}" y1="${y}" x2="${xEnd}" y2="${y}" stroke="${project.color}" stroke-width="${sw}" stroke-opacity="0.85" stroke-linecap="round"/>`,
    );

    // Project label (counter-scale so font stays legible)
    const labelScale = 1 / scale;
    parts.push(
      `<g transform="translate(${xStart},${y - 22}) scale(${labelScale})">`,
    );
    parts.push(
      `<rect x="-4" y="-2" width="${Math.max(project.name.length * 8 + 12, 60)}" height="20" fill="#0a0a0a" stroke="${project.color}" stroke-width="1" rx="3" fill-opacity="0.92"/>`,
    );
    parts.push(
      `<text x="4" y="14" fill="#e5e5e5" font-size="13">${escapeXML(project.name)}</text>`,
    );
    parts.push(`</g>`);

    // Feature orbs from the loaded detail (if available)
    const detail = details[project.slug];
    if (detail) {
      for (const f of detail.features) {
        const xCreated = xForTime(f.created_at);
        const xCompleted = f.completed_at != null ? xForTime(f.completed_at) : null;
        const color = STATUS_COLORS[f.status];
        const baseOpacity = f.status === "dropped" ? 0.35 : 0.95;

        if (xCompleted != null) {
          parts.push(
            `<line x1="${xCreated}" y1="${y}" x2="${xCompleted}" y2="${y}" stroke="${color}" stroke-width="1.5" stroke-opacity="0.5"/>`,
          );
        }

        // Created orb
        parts.push(
          `<circle cx="${xCreated}" cy="${y}" r="${ORB_RADIUS}" stroke="${color}" stroke-width="1.5" fill="${
            f.status === "done" ? color : "transparent"
          }" fill-opacity="${baseOpacity}" stroke-opacity="${baseOpacity}"/>`,
        );

        // Completed orb (filled)
        if (xCompleted != null) {
          parts.push(
            `<circle cx="${xCompleted}" cy="${y}" r="${ORB_RADIUS}" fill="${color}" fill-opacity="${baseOpacity}"/>`,
          );
        }
      }
    }
  }

  // Now line
  const xNow = xForTime(now);
  parts.push(
    `<line x1="${xNow}" y1="${yMin}" x2="${xNow}" y2="${yMax}" stroke="#7aa2f7" stroke-width="1.2" stroke-opacity="0.6" stroke-dasharray="4 4"/>`,
  );
  parts.push(
    `<circle cx="${xNow}" cy="${yMin}" r="3" fill="#7aa2f7" fill-opacity="0.8"/>`,
  );

  parts.push(`</g>`);

  // Footer caption
  const stamp = new Date().toISOString().slice(0, 10);
  parts.push(
    `<text x="${width - 12}" y="${height - 12}" text-anchor="end" fill="#525252" font-size="11">petboard universe · ${stamp}</text>`,
  );

  parts.push(`</svg>`);
  return parts.join("");
}

/**
 * Trigger a browser download of the SVG. The blob URL is revoked
 * immediately after the click — there's no point keeping it around.
 */
export function downloadSVG(svg: string, filename: string) {
  const blob = new Blob([svg], { type: "image/svg+xml;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}
