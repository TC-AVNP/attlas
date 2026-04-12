import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api/client";
import type { Category } from "../api/types";

function formatAmount(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function ExpenseTimeline({ groupId }: { groupId: number }) {
  const [category, setCategory] = useState("");
  const [search, setSearch] = useState("");

  const { data: entries, isLoading } = useQuery({
    queryKey: ["timeline", groupId, category, search],
    queryFn: () => api.getTimeline(groupId, category, search),
  });
  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: api.listCategories,
  });

  // Group entries by month
  const byMonth: Record<string, typeof entries> = {};
  if (entries) {
    for (const e of entries) {
      const month = new Date(e.created_at * 1000).toLocaleDateString("en-US", {
        month: "long",
        year: "numeric",
      });
      if (!byMonth[month]) byMonth[month] = [];
      byMonth[month]!.push(e);
    }
  }

  return (
    <div>
      {/* Filters */}
      <div className="flex gap-2 mb-4">
        <select
          value={category}
          onChange={(e) => setCategory(e.target.value)}
          className="border rounded px-3 py-2 text-sm"
        >
          <option value="">All categories</option>
          {categories?.map((c: Category) => (
            <option key={c.id} value={c.name}>
              {c.name}
            </option>
          ))}
        </select>
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search..."
          className="flex-1 border rounded px-3 py-2 text-sm"
        />
      </div>

      {isLoading && <p className="text-gray-400">Loading timeline...</p>}

      {entries && entries.length === 0 && (
        <p className="text-gray-400 text-center py-8">No entries found.</p>
      )}

      {Object.entries(byMonth).map(([month, items]) => (
        <div key={month} className="mb-6">
          <h3 className="text-sm font-semibold text-gray-500 mb-2">{month}</h3>
          <div className="space-y-2">
            {items!.map((e) => (
              <div
                key={`${e.type}-${e.id}`}
                className={`bg-white rounded-lg shadow-sm border p-3 ${
                  e.type === "settlement" ? "border-l-4 border-l-green-400" : ""
                }`}
              >
                <div className="flex justify-between items-start">
                  <div>
                    <p className="font-medium text-sm">{e.description}</p>
                    <p className="text-xs text-gray-400">
                      {e.type === "expense"
                        ? `${e.paid_by_name} paid`
                        : "Settlement"}
                      {e.category && (
                        <span className="ml-2 px-1.5 py-0.5 bg-gray-100 rounded">
                          {e.category}
                        </span>
                      )}
                    </p>
                  </div>
                  <div className="text-right">
                    <p
                      className={`font-semibold text-sm ${e.type === "settlement" ? "text-green-600" : ""}`}
                    >
                      {formatAmount(e.amount)}
                    </p>
                    <p className="text-xs text-gray-400">
                      {new Date(e.created_at * 1000).toLocaleDateString()}
                    </p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}
