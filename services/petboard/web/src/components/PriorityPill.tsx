import type { Priority } from "../api/types";

const STYLES: Record<Priority, string> = {
  high: "bg-red-500/20 text-red-300 border-red-500/40",
  medium: "bg-amber-500/20 text-amber-300 border-amber-500/40",
  low: "bg-sky-500/20 text-sky-300 border-sky-500/40",
};

export default function PriorityPill({ priority }: { priority: Priority }) {
  return (
    <span
      className={`px-2 py-0.5 text-xs uppercase tracking-wide rounded border ${STYLES[priority]}`}
    >
      {priority}
    </span>
  );
}
