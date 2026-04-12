import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api/client";
import type { User } from "../api/types";

function formatAmount(cents: number): string {
  return (cents / 100).toFixed(2);
}

export default function SettleUp({
  groupId,
  members,
}: {
  groupId: number;
  members: User[];
}) {
  const qc = useQueryClient();

  const { data: suggestions, isLoading } = useQuery({
    queryKey: ["suggestions", groupId],
    queryFn: () => api.suggestPayments(groupId),
  });

  const [showManual, setShowManual] = useState(false);
  const [fromUser, setFromUser] = useState("");
  const [toUser, setToUser] = useState("");
  const [amount, setAmount] = useState("");

  const settleMut = useMutation({
    mutationFn: ({
      from,
      to,
      amt,
    }: {
      from: number;
      to: number;
      amt: number;
    }) => api.addSettlement(groupId, from, to, amt),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["suggestions"] });
      qc.invalidateQueries({ queryKey: ["balances"] });
      qc.invalidateQueries({ queryKey: ["timeline"] });
      setShowManual(false);
      setAmount("");
    },
  });

  if (isLoading) return <p className="text-gray-400">Loading...</p>;

  return (
    <div>
      {/* Suggested payments */}
      <h3 className="text-sm font-semibold text-gray-500 mb-3">
        Suggested Payments
      </h3>
      {!suggestions?.length ? (
        <p className="text-gray-400 text-center py-4 mb-4">
          All settled up! No payments needed.
        </p>
      ) : (
        <div className="space-y-2 mb-6">
          {suggestions.map((s, i) => (
            <div
              key={i}
              className="bg-white rounded-lg shadow-sm border p-4 flex items-center justify-between"
            >
              <div>
                <p className="font-medium text-sm">
                  {s.from_user_name} pays {s.to_user_name}
                </p>
                <p className="text-lg font-bold text-brand">
                  {formatAmount(s.amount)}
                </p>
              </div>
              <button
                onClick={() =>
                  settleMut.mutate({
                    from: s.from_user,
                    to: s.to_user,
                    amt: s.amount,
                  })
                }
                disabled={settleMut.isPending}
                className="bg-green-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-green-700 disabled:opacity-50"
              >
                Record Payment
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Manual settlement */}
      <button
        onClick={() => setShowManual(!showManual)}
        className="text-sm text-brand hover:underline mb-3"
      >
        {showManual ? "Hide manual entry" : "Record a manual payment"}
      </button>

      {showManual && (
        <div className="bg-white rounded-lg shadow-sm border p-4 space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs text-gray-500 mb-1">From</label>
              <select
                value={fromUser}
                onChange={(e) => setFromUser(e.target.value)}
                className="w-full border rounded px-3 py-2 text-sm"
              >
                <option value="">Select...</option>
                {members.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name || m.email}
                  </option>
                ))}
              </select>
            </div>
            <div>
              <label className="block text-xs text-gray-500 mb-1">To</label>
              <select
                value={toUser}
                onChange={(e) => setToUser(e.target.value)}
                className="w-full border rounded px-3 py-2 text-sm"
              >
                <option value="">Select...</option>
                {members.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name || m.email}
                  </option>
                ))}
              </select>
            </div>
          </div>
          <div>
            <label className="block text-xs text-gray-500 mb-1">Amount</label>
            <input
              type="number"
              step="0.01"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              placeholder="0.00"
              className="w-full border rounded px-3 py-2 text-sm"
            />
          </div>
          <button
            onClick={() =>
              settleMut.mutate({
                from: Number(fromUser),
                to: Number(toUser),
                amt: Math.round(parseFloat(amount) * 100),
              })
            }
            disabled={
              !fromUser ||
              !toUser ||
              fromUser === toUser ||
              !amount ||
              settleMut.isPending
            }
            className="bg-green-600 text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
          >
            {settleMut.isPending ? "Recording..." : "Record Payment"}
          </button>
          {settleMut.isError && (
            <p className="text-red-500 text-sm">
              {(settleMut.error as Error).message}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
