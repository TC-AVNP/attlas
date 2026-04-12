import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api/client";

function formatAmount(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function Overview() {
  const { data: months, isLoading } = useQuery({
    queryKey: ["overview"],
    queryFn: api.getOverview,
  });

  const [expandedMonth, setExpandedMonth] = useState<string | null>(null);

  if (isLoading) return <p className="text-gray-400">Loading overview...</p>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Monthly Spending</h1>

      {!months?.length ? (
        <p className="text-gray-400 text-center py-8">
          No spending data yet. Add some expenses!
        </p>
      ) : (
        <div className="space-y-2">
          {months.map((m) => (
            <MonthCard
              key={m.month}
              month={m.month}
              total={m.total}
              byGroup={m.by_group}
              isExpanded={expandedMonth === m.month}
              onToggle={() =>
                setExpandedMonth(
                  expandedMonth === m.month ? null : m.month,
                )
              }
            />
          ))}
        </div>
      )}
    </div>
  );
}

function MonthCard({
  month,
  total,
  byGroup,
  isExpanded,
  onToggle,
}: {
  month: string;
  total: number;
  byGroup: { group_id: number; group_name: string; total: number }[];
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const { data: detail } = useQuery({
    queryKey: ["overview", month],
    queryFn: () => api.getOverviewMonth(month),
    enabled: isExpanded,
  });

  const label = new Date(month + "-01").toLocaleDateString("en-US", {
    month: "long",
    year: "numeric",
  });

  return (
    <div className="bg-white rounded-lg shadow-sm border">
      <button
        onClick={onToggle}
        className="w-full flex items-center justify-between p-4 text-left hover:bg-gray-50"
      >
        <span className="font-medium">{label}</span>
        <span className="font-semibold">{formatAmount(total)}</span>
      </button>

      {isExpanded && (
        <div className="border-t px-4 pb-4 pt-3">
          {/* By group */}
          <h4 className="text-xs font-semibold text-gray-500 mb-2">
            By Group
          </h4>
          <div className="space-y-1 mb-4">
            {byGroup.map((g) => (
              <div key={g.group_id} className="flex justify-between text-sm">
                <span className="text-gray-600">{g.group_name}</span>
                <span>{formatAmount(g.total)}</span>
              </div>
            ))}
          </div>

          {/* By category (from detail query) */}
          {detail?.by_category && detail.by_category.length > 0 && (
            <>
              <h4 className="text-xs font-semibold text-gray-500 mb-2">
                By Category
              </h4>
              <div className="space-y-1">
                {detail.by_category.map((c) => (
                  <div
                    key={c.category_id}
                    className="flex justify-between text-sm"
                  >
                    <span className="text-gray-600">{c.category_name}</span>
                    <span>{formatAmount(c.total)}</span>
                  </div>
                ))}
              </div>
            </>
          )}
        </div>
      )}
    </div>
  );
}
