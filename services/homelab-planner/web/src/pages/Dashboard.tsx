import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { ChecklistItem } from "../api/types";
import { formatCents } from "../lib/format";
import StatusBadge from "../components/StatusBadge";

export default function Dashboard() {
  const queryClient = useQueryClient();

  const { data, isLoading } = useQuery({
    queryKey: ["allItems"],
    queryFn: api.listAllItems,
  });

  const items = data?.items ?? [];

  const totalCost = items.reduce(
    (sum, i) => sum + (i.actual_cost_cents ?? 0),
    0,
  );
  const arrivedCount = items.filter((i) => i.status === "arrived").length;

  // Group by delivery date
  const groups = new Map<string, ChecklistItem[]>();
  for (const item of items) {
    const key = item.delivery_date || "No date";
    if (!groups.has(key)) groups.set(key, []);
    groups.get(key)!.push(item);
  }

  // Sort date groups
  const sortedEntries = Array.from(groups.entries()).sort(([a], [b]) => {
    if (a === "No date") return 1;
    if (b === "No date") return -1;
    return a.localeCompare(b);
  });

  if (isLoading) {
    return <div className="p-8 text-gray-500">Loading...</div>;
  }

  return (
    <div className="max-w-4xl mx-auto p-6">
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Homelab Delivery Tracker</h1>
        <div className="flex items-center gap-3">
          <Link
            to="/schematic"
            className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded text-sm text-gray-300"
          >
            Schematic
          </Link>
          <Link
            to="/schematic-3d"
            className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded text-sm text-gray-300"
          >
            3D View
          </Link>
          <Link
            to="/steps"
            className="px-3 py-1.5 bg-gray-800 hover:bg-gray-700 rounded text-sm text-gray-300"
          >
            Steps
          </Link>
        </div>
      </div>

      {/* Summary bar */}
      <div className="grid grid-cols-3 gap-4 mb-8">
        <div className="bg-gray-900 rounded-lg p-4">
          <div className="text-sm text-gray-400">Total Spent</div>
          <div className="text-xl font-bold text-green-400">
            &euro;{formatCents(totalCost)}
          </div>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <div className="text-sm text-gray-400">Items</div>
          <div className="text-xl font-bold text-indigo-400">
            {items.length}
          </div>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <div className="text-sm text-gray-400">Arrived</div>
          <div className="text-xl font-bold text-emerald-400">
            {arrivedCount} / {items.length}
          </div>
          {items.length > 0 && (
            <div className="mt-2 bg-gray-800 rounded-full h-2">
              <div
                className="bg-emerald-500 rounded-full h-2 transition-all"
                style={{
                  width: `${(arrivedCount / items.length) * 100}%`,
                }}
              />
            </div>
          )}
        </div>
      </div>

      {/* Delivery groups */}
      <div className="space-y-6">
        {sortedEntries.map(([date, groupItems]) => {
          const allArrived = groupItems.every((i) => i.status === "arrived");
          return (
            <div key={date}>
              <div className="flex items-center gap-3 mb-3">
                <h2 className="text-sm font-semibold uppercase tracking-wider text-amber-400">
                  {formatDeliveryDate(date)}
                </h2>
                {allArrived && (
                  <span className="text-xs text-emerald-400 bg-emerald-900/30 px-2 py-0.5 rounded">
                    All arrived
                  </span>
                )}
              </div>
              <div className="space-y-2">
                {groupItems.map((item) => (
                  <DeliveryItemCard
                    key={item.id}
                    item={item}
                    onMutate={() =>
                      queryClient.invalidateQueries({
                        queryKey: ["allItems"],
                      })
                    }
                  />
                ))}
              </div>
            </div>
          );
        })}
      </div>

      {items.length === 0 && (
        <p className="text-gray-500 text-center py-12">
          No items ordered yet.
        </p>
      )}
    </div>
  );
}

function DeliveryItemCard({
  item,
  onMutate,
}: {
  item: ChecklistItem;
  onMutate: () => void;
}) {
  const markArrived = useMutation({
    mutationFn: () => api.updateItem(item.id, { status: "arrived" }),
    onSuccess: onMutate,
  });

  const markOrdered = useMutation({
    mutationFn: () => api.updateItem(item.id, { status: "ordered" }),
    onSuccess: onMutate,
  });

  const isArrived = item.status === "arrived";
  const selectedOption = item.options?.find(
    (o) => o.id === item.selected_option_id,
  );

  return (
    <div
      className={`flex items-center gap-4 rounded-lg p-4 transition-colors ${
        isArrived
          ? "bg-emerald-900/20 border border-emerald-800/50"
          : "bg-gray-900 hover:bg-gray-800"
      }`}
    >
      {/* Checkbox */}
      <button
        onClick={() =>
          isArrived ? markOrdered.mutate() : markArrived.mutate()
        }
        className={`w-6 h-6 rounded border-2 flex-shrink-0 flex items-center justify-center transition-colors ${
          isArrived
            ? "border-emerald-400 bg-emerald-400 text-gray-900"
            : "border-gray-500 hover:border-emerald-400"
        }`}
      >
        {isArrived && (
          <svg
            width="14"
            height="14"
            viewBox="0 0 14 14"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="2,7 5.5,10.5 12,3.5" />
          </svg>
        )}
      </button>

      {/* Item info */}
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={`font-medium ${
              isArrived ? "line-through text-gray-500" : ""
            }`}
          >
            {item.name}
          </span>
          <span className="text-xs text-gray-600 bg-gray-800 px-1.5 py-0.5 rounded">
            {item.group_name}
          </span>
        </div>
        {selectedOption && (
          <div className="flex items-center gap-2 mt-1 text-xs text-gray-500">
            <span>{selectedOption.name}</span>
            {selectedOption.url && (
              <a
                href={selectedOption.url}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-400 hover:underline"
              >
                link
              </a>
            )}
          </div>
        )}
      </div>

      {/* Price */}
      <div className="text-right flex-shrink-0">
        {item.actual_cost_cents != null && (
          <div className="text-sm font-medium text-gray-300">
            &euro;{formatCents(item.actual_cost_cents)}
          </div>
        )}
      </div>

      {/* Status */}
      <StatusBadge status={item.status} />
    </div>
  );
}

function formatDeliveryDate(date: string): string {
  if (date === "No date") return "No delivery date";
  try {
    const d = new Date(date + "T00:00:00");
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const diff = Math.round(
      (d.getTime() - today.getTime()) / (1000 * 60 * 60 * 24),
    );

    const formatted = d.toLocaleDateString("en-GB", {
      weekday: "long",
      day: "numeric",
      month: "long",
    });

    if (diff === 0) return `Today — ${formatted}`;
    if (diff === 1) return `Tomorrow — ${formatted}`;
    if (diff === -1) return `Yesterday — ${formatted}`;
    if (diff < 0) return `${formatted} (${Math.abs(diff)} days ago)`;
    return `${formatted} (in ${diff} days)`;
  } catch {
    return date;
  }
}
