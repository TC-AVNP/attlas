import type { ItemStatus } from "../api/types";

const styles: Record<ItemStatus, string> = {
  researching: "bg-yellow-900 text-yellow-200",
  ordered: "bg-blue-900 text-blue-200",
  arrived: "bg-green-900 text-green-200",
};

export default function StatusBadge({ status }: { status: ItemStatus }) {
  return (
    <span
      className={`px-2 py-0.5 rounded text-xs font-medium ${styles[status]}`}
    >
      {status}
    </span>
  );
}
