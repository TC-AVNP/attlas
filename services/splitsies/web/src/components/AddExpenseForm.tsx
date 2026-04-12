import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api/client";
import type { Category, User } from "../api/types";

export default function AddExpenseForm({
  groupId,
  members,
  categories,
  onDone,
}: {
  groupId: number;
  members: User[];
  categories: Category[];
  onDone: () => void;
}) {
  const qc = useQueryClient();
  const [paidBy, setPaidBy] = useState(members[0]?.id.toString() ?? "");
  const [amount, setAmount] = useState("");
  const [description, setDescription] = useState("");
  const [categoryId, setCategoryId] = useState("");
  const [splitType, setSplitType] = useState<"even" | "custom" | "percentage">(
    "even",
  );
  const [selectedMembers, setSelectedMembers] = useState<Set<number>>(
    new Set(members.map((m) => m.id)),
  );
  const [customAmounts, setCustomAmounts] = useState<Record<number, string>>(
    {},
  );
  const [newCategory, setNewCategory] = useState("");

  const addExpMut = useMutation({
    mutationFn: () => {
      const amountCents = Math.round(parseFloat(amount) * 100);

      let splits:
        | { user_id: number; amount: number }[]
        | undefined;

      if (splitType === "even") {
        splits = Array.from(selectedMembers).map((uid) => ({
          user_id: uid,
          amount: 0,
        }));
      } else if (splitType === "custom") {
        splits = Array.from(selectedMembers).map((uid) => ({
          user_id: uid,
          amount: Math.round(parseFloat(customAmounts[uid] || "0") * 100),
        }));
      } else {
        // percentage — amount field is percentage, convert to basis points
        splits = Array.from(selectedMembers).map((uid) => ({
          user_id: uid,
          amount: Math.round(parseFloat(customAmounts[uid] || "0") * 100),
        }));
      }

      return api.addExpense(groupId, {
        paid_by: Number(paidBy),
        amount: amountCents,
        description,
        category_id: categoryId ? Number(categoryId) : undefined,
        split_type: splitType,
        splits,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["expenses"] });
      qc.invalidateQueries({ queryKey: ["balances"] });
      qc.invalidateQueries({ queryKey: ["timeline"] });
      qc.invalidateQueries({ queryKey: ["overview"] });
      onDone();
    },
  });

  const createCatMut = useMutation({
    mutationFn: () => api.createCategory(newCategory),
    onSuccess: (cat) => {
      qc.invalidateQueries({ queryKey: ["categories"] });
      setCategoryId(cat.id.toString());
      setNewCategory("");
    },
  });

  const toggleMember = (uid: number) => {
    const next = new Set(selectedMembers);
    if (next.has(uid)) next.delete(uid);
    else next.add(uid);
    setSelectedMembers(next);
  };

  return (
    <div className="bg-white rounded-lg shadow-sm border p-4 mb-4 space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-gray-500 mb-1">Paid by</label>
          <select
            value={paidBy}
            onChange={(e) => setPaidBy(e.target.value)}
            className="w-full border rounded px-3 py-2 text-sm"
          >
            {members.map((m) => (
              <option key={m.id} value={m.id}>
                {m.name || m.email}
              </option>
            ))}
          </select>
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
      </div>

      <div>
        <label className="block text-xs text-gray-500 mb-1">Description</label>
        <input
          type="text"
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          placeholder="What was this for?"
          className="w-full border rounded px-3 py-2 text-sm"
        />
      </div>

      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-gray-500 mb-1">Category</label>
          <select
            value={categoryId}
            onChange={(e) => setCategoryId(e.target.value)}
            className="w-full border rounded px-3 py-2 text-sm"
          >
            <option value="">None</option>
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </div>
        <div>
          <label className="block text-xs text-gray-500 mb-1">
            Or create new
          </label>
          <div className="flex gap-1">
            <input
              type="text"
              value={newCategory}
              onChange={(e) => setNewCategory(e.target.value)}
              placeholder="New category"
              className="flex-1 border rounded px-3 py-2 text-sm"
            />
            <button
              onClick={() => createCatMut.mutate()}
              disabled={!newCategory.trim()}
              className="px-3 py-2 bg-gray-100 rounded text-sm disabled:opacity-50"
            >
              +
            </button>
          </div>
        </div>
      </div>

      <div>
        <label className="block text-xs text-gray-500 mb-1">Split type</label>
        <div className="flex gap-2">
          {(["even", "custom", "percentage"] as const).map((t) => (
            <button
              key={t}
              onClick={() => setSplitType(t)}
              className={`px-3 py-1 rounded text-sm ${
                splitType === t
                  ? "bg-brand text-white"
                  : "bg-gray-100 text-gray-600"
              }`}
            >
              {t === "even" ? "Even" : t === "custom" ? "Custom" : "Percentage"}
            </button>
          ))}
        </div>
      </div>

      {/* Member selection for even split */}
      {splitType === "even" && (
        <div>
          <label className="block text-xs text-gray-500 mb-1">
            Split among
          </label>
          <div className="flex flex-wrap gap-2">
            {members.map((m) => (
              <button
                key={m.id}
                onClick={() => toggleMember(m.id)}
                className={`px-3 py-1 rounded text-sm ${
                  selectedMembers.has(m.id)
                    ? "bg-brand text-white"
                    : "bg-gray-100 text-gray-500"
                }`}
              >
                {m.name || m.email}
              </button>
            ))}
          </div>
        </div>
      )}

      {/* Custom amounts */}
      {(splitType === "custom" || splitType === "percentage") && (
        <div>
          <label className="block text-xs text-gray-500 mb-1">
            {splitType === "custom"
              ? "Amount per person"
              : "Percentage per person"}
          </label>
          <div className="space-y-1">
            {members.map((m) => (
              <div key={m.id} className="flex items-center gap-2">
                <button
                  onClick={() => toggleMember(m.id)}
                  className={`w-4 h-4 rounded border ${selectedMembers.has(m.id) ? "bg-brand border-brand" : ""}`}
                />
                <span className="text-sm flex-1">{m.name || m.email}</span>
                {selectedMembers.has(m.id) && (
                  <input
                    type="number"
                    step={splitType === "custom" ? "0.01" : "1"}
                    value={customAmounts[m.id] || ""}
                    onChange={(e) =>
                      setCustomAmounts({
                        ...customAmounts,
                        [m.id]: e.target.value,
                      })
                    }
                    placeholder={splitType === "custom" ? "0.00" : "0%"}
                    className="w-24 border rounded px-2 py-1 text-sm text-right"
                  />
                )}
              </div>
            ))}
          </div>
        </div>
      )}

      <div className="flex gap-2 pt-2">
        <button
          onClick={() => addExpMut.mutate()}
          disabled={
            !amount ||
            !description.trim() ||
            selectedMembers.size === 0 ||
            addExpMut.isPending
          }
          className="bg-brand text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
        >
          {addExpMut.isPending ? "Adding..." : "Add Expense"}
        </button>
        <button
          onClick={onDone}
          className="px-4 py-2 rounded text-sm text-gray-500 hover:text-gray-700"
        >
          Cancel
        </button>
      </div>
      {addExpMut.isError && (
        <p className="text-red-500 text-sm">
          {(addExpMut.error as Error).message}
        </p>
      )}
    </div>
  );
}
