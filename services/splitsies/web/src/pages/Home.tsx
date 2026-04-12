import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";

function formatAmount(cents: number): string {
  const abs = Math.abs(cents);
  return (cents < 0 ? "-" : "") + (abs / 100).toFixed(2);
}

export default function Home() {
  const { data, isLoading } = useQuery({
    queryKey: ["balances"],
    queryFn: api.getMyBalances,
  });

  const [expanded, setExpanded] = useState<number | null>(null);

  if (isLoading) return <p className="text-gray-400">Loading balances...</p>;
  if (!data) return null;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">Dashboard</h1>

      {/* Net position */}
      <div className="bg-white rounded-lg shadow-sm border p-6 mb-6">
        <p className="text-sm text-gray-500 mb-1">Your net position</p>
        <p
          className={`text-3xl font-bold ${
            data.total_net > 0
              ? "text-green-600"
              : data.total_net < 0
                ? "text-red-600"
                : "text-gray-600"
          }`}
        >
          {data.total_net > 0 && "+"}
          {formatAmount(data.total_net)}
        </p>
        <p className="text-xs text-gray-400 mt-1">
          {data.total_net > 0
            ? "Others owe you"
            : data.total_net < 0
              ? "You owe others"
              : "All settled up!"}
        </p>
      </div>

      {/* Per-person balances */}
      {data.balances.length === 0 ? (
        <p className="text-gray-400 text-center py-8">
          No balances yet.{" "}
          <Link to="/groups" className="text-brand hover:underline">
            Join a group
          </Link>{" "}
          and add some expenses!
        </p>
      ) : (
        <div className="space-y-2">
          {data.balances.map((b) => (
            <div
              key={b.user_id}
              className="bg-white rounded-lg shadow-sm border"
            >
              <button
                onClick={() =>
                  setExpanded(expanded === b.user_id ? null : b.user_id)
                }
                className="w-full flex items-center justify-between p-4 text-left hover:bg-gray-50"
              >
                <span className="font-medium">{b.user_name}</span>
                <span
                  className={`font-semibold ${b.net > 0 ? "text-green-600" : "text-red-600"}`}
                >
                  {b.net > 0 ? "owes you " : "you owe "}
                  {formatAmount(Math.abs(b.net))}
                </span>
              </button>
              {expanded === b.user_id && b.groups && (
                <div className="border-t px-4 pb-3 pt-2 space-y-1">
                  {b.groups.map((g) => (
                    <div
                      key={g.group_id}
                      className="flex justify-between text-sm"
                    >
                      <Link
                        to={`/groups/${g.group_id}`}
                        className="text-gray-600 hover:text-brand"
                      >
                        {g.group_name}
                      </Link>
                      <span
                        className={
                          g.net > 0 ? "text-green-600" : "text-red-600"
                        }
                      >
                        {g.net > 0 ? "+" : ""}
                        {formatAmount(g.net)}
                      </span>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
