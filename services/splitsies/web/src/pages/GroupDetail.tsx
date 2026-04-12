import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { useParams } from "react-router-dom";
import { api } from "../api/client";
import type { User } from "../api/types";
import AddExpenseForm from "../components/AddExpenseForm";
import ExpenseTimeline from "../components/ExpenseTimeline";
import SettleUp from "../components/SettleUp";

function formatAmount(cents: number): string {
  return (Math.abs(cents) / 100).toFixed(2);
}

type Tab = "expenses" | "timeline" | "balances" | "settle" | "members";

export default function GroupDetail() {
  const { id } = useParams<{ id: string }>();
  const groupId = Number(id);
  const qc = useQueryClient();

  const { data: group, isLoading } = useQuery({
    queryKey: ["groups", groupId],
    queryFn: () => api.getGroup(groupId),
  });
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });
  const { data: users } = useQuery({
    queryKey: ["users"],
    queryFn: api.listUsers,
  });
  const { data: categories } = useQuery({
    queryKey: ["categories"],
    queryFn: api.listCategories,
  });
  const { data: balances } = useQuery({
    queryKey: ["balances", "group", groupId],
    queryFn: () => api.getGroupBalances(groupId),
  });

  const [tab, setTab] = useState<Tab>("expenses");
  const [showAddExpense, setShowAddExpense] = useState(false);
  const [addMemberUserId, setAddMemberUserId] = useState("");

  const addMemberMut = useMutation({
    mutationFn: () => api.addGroupMember(groupId, Number(addMemberUserId)),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["groups"] });
      setAddMemberUserId("");
    },
  });

  if (isLoading) return <p className="text-gray-400">Loading...</p>;
  if (!group) return <p className="text-red-500">Group not found</p>;

  const isCreator = me?.id === group.created_by;
  const memberIds = new Set(group.members.map((m: User) => m.id));
  const nonMembers = (users ?? []).filter(
    (u: User) => u.is_active && !memberIds.has(u.id),
  );

  const tabs: { key: Tab; label: string }[] = [
    { key: "expenses", label: "Expenses" },
    { key: "timeline", label: "Timeline" },
    { key: "balances", label: "Balances" },
    { key: "settle", label: "Settle Up" },
    { key: "members", label: "Members" },
  ];

  return (
    <div>
      <h1 className="text-2xl font-bold mb-1">{group.name}</h1>
      {group.description && (
        <p className="text-gray-500 text-sm mb-4">{group.description}</p>
      )}

      {/* Tabs */}
      <div className="flex gap-1 mb-6 border-b">
        {tabs.map((t) => (
          <button
            key={t.key}
            onClick={() => setTab(t.key)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px ${
              tab === t.key
                ? "border-brand text-brand"
                : "border-transparent text-gray-500 hover:text-gray-700"
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>

      {/* Expenses tab */}
      {tab === "expenses" && (
        <div>
          <button
            onClick={() => setShowAddExpense(!showAddExpense)}
            className="bg-brand text-white px-4 py-2 rounded-lg text-sm font-medium mb-4 hover:bg-brand/90"
          >
            {showAddExpense ? "Cancel" : "Add Expense"}
          </button>

          {showAddExpense && (
            <AddExpenseForm
              groupId={groupId}
              members={group.members}
              categories={categories ?? []}
              onDone={() => setShowAddExpense(false)}
            />
          )}

          <ExpenseList groupId={groupId} />
        </div>
      )}

      {/* Timeline tab */}
      {tab === "timeline" && <ExpenseTimeline groupId={groupId} />}

      {/* Balances tab */}
      {tab === "balances" && (
        <div className="space-y-2">
          {!balances?.length ? (
            <p className="text-gray-400 text-center py-8">
              No balances yet. Add some expenses!
            </p>
          ) : (
            balances.map((b) => (
              <div
                key={b.user_id}
                className="bg-white rounded-lg shadow-sm border p-4 flex justify-between"
              >
                <span>{b.user_name}</span>
                <span
                  className={`font-semibold ${b.net > 0 ? "text-green-600" : b.net < 0 ? "text-red-600" : "text-gray-400"}`}
                >
                  {b.net > 0 ? "+" : ""}
                  {formatAmount(b.net)}
                </span>
              </div>
            ))
          )}
        </div>
      )}

      {/* Settle Up tab */}
      {tab === "settle" && (
        <SettleUp groupId={groupId} members={group.members} />
      )}

      {/* Members tab */}
      {tab === "members" && (
        <div>
          <div className="space-y-2 mb-4">
            {group.members.map((m: User) => (
              <div
                key={m.id}
                className="bg-white rounded-lg shadow-sm border p-3 flex items-center gap-3"
              >
                {m.picture && (
                  <img
                    src={m.picture}
                    alt=""
                    className="w-8 h-8 rounded-full"
                  />
                )}
                <div>
                  <p className="font-medium text-sm">
                    {m.name || m.email}
                    {m.id === group.created_by && (
                      <span className="ml-2 text-xs text-brand">(creator)</span>
                    )}
                  </p>
                  <p className="text-xs text-gray-400">{m.email}</p>
                </div>
              </div>
            ))}
          </div>

          {isCreator && nonMembers.length > 0 && (
            <div className="bg-white rounded-lg shadow-sm border p-4">
              <h3 className="text-sm font-semibold mb-2">Add Member</h3>
              <div className="flex gap-2">
                <select
                  value={addMemberUserId}
                  onChange={(e) => setAddMemberUserId(e.target.value)}
                  className="flex-1 border rounded px-3 py-2 text-sm"
                >
                  <option value="">Select user...</option>
                  {nonMembers.map((u: User) => (
                    <option key={u.id} value={u.id}>
                      {u.name || u.email}
                    </option>
                  ))}
                </select>
                <button
                  onClick={() => addMemberMut.mutate()}
                  disabled={!addMemberUserId || addMemberMut.isPending}
                  className="bg-brand text-white px-4 py-2 rounded text-sm disabled:opacity-50"
                >
                  Add
                </button>
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function ExpenseList({ groupId }: { groupId: number }) {
  const qc = useQueryClient();
  const { data: expenses, isLoading } = useQuery({
    queryKey: ["expenses", groupId],
    queryFn: () => api.listExpenses(groupId),
  });

  const deleteMut = useMutation({
    mutationFn: (id: number) => api.deleteExpense(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["expenses"] });
      qc.invalidateQueries({ queryKey: ["balances"] });
      qc.invalidateQueries({ queryKey: ["timeline"] });
    },
  });

  if (isLoading) return <p className="text-gray-400">Loading expenses...</p>;
  if (!expenses?.length)
    return (
      <p className="text-gray-400 text-center py-8">No expenses yet.</p>
    );

  return (
    <div className="space-y-2">
      {expenses.map((e) => (
        <div key={e.id} className="bg-white rounded-lg shadow-sm border p-4">
          <div className="flex items-start justify-between">
            <div>
              <p className="font-medium">{e.description}</p>
              <p className="text-sm text-gray-500">
                {e.paid_by_name} paid {formatAmount(e.amount)}
                {e.category && (
                  <span className="ml-2 px-2 py-0.5 bg-gray-100 rounded text-xs">
                    {e.category}
                  </span>
                )}
              </p>
              {e.splits && (
                <div className="mt-1 text-xs text-gray-400">
                  Split ({e.split_type}):{" "}
                  {e.splits
                    .map((s) => `${s.user_name} ${formatAmount(s.amount)}`)
                    .join(", ")}
                </div>
              )}
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-gray-400">
                {new Date(e.created_at * 1000).toLocaleDateString()}
              </span>
              <button
                onClick={() => {
                  if (confirm("Delete this expense?")) deleteMut.mutate(e.id);
                }}
                className="text-red-400 hover:text-red-600 text-xs"
              >
                Delete
              </button>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
