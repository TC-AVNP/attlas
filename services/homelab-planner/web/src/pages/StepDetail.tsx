import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams, Link } from "react-router-dom";
import { useState } from "react";
import { api } from "../api/client";
import type { ChecklistItem, ItemOption, ItemStatus } from "../api/types";
import { formatCents, formatDateTime } from "../lib/format";
import StatusBadge from "../components/StatusBadge";

export default function StepDetail() {
  const { id } = useParams<{ id: string }>();
  const stepId = Number(id);
  const queryClient = useQueryClient();

  const { data: step, isLoading } = useQuery({
    queryKey: ["step", stepId],
    queryFn: () => api.getStep(stepId),
  });

  const invalidate = () =>
    queryClient.invalidateQueries({ queryKey: ["step", stepId] });

  if (isLoading || !step) {
    return <div className="p-8 text-gray-500">Loading...</div>;
  }

  return (
    <div className="max-w-4xl mx-auto p-6">
      <Link to="/" className="text-sm text-gray-400 hover:text-gray-200 mb-4 inline-block">
        &larr; Back
      </Link>

      <div className="mb-6">
        <h1 className="text-2xl font-bold mb-1">{step.title}</h1>
        {step.description && (
          <p className="text-gray-400">{step.description}</p>
        )}
        <div className="flex gap-6 mt-2 text-sm text-gray-500">
          <span>Budget: &euro;{formatCents(step.budget_cents)}</span>
          <span>Spent: &euro;{formatCents(step.actual_cents)}</span>
          <span>
            {step.arrived_count}/{step.item_count} arrived
          </span>
        </div>
      </div>

      {/* Checklist */}
      <section className="mb-8">
        <h2 className="text-lg font-semibold mb-3">Checklist</h2>
        <div className="space-y-3">
          {step.items.map((item) => (
            <ItemCard key={item.id} item={item} onMutate={invalidate} />
          ))}
        </div>
        <AddItemForm stepId={stepId} onCreated={invalidate} />
      </section>

      {/* Build Log */}
      <section>
        <h2 className="text-lg font-semibold mb-3">Build Log</h2>
        <div className="space-y-3">
          {step.build_log.map((entry) => (
            <div key={entry.id} className="bg-gray-900 rounded-lg p-4">
              <div className="text-xs text-gray-500 mb-1">
                {formatDateTime(entry.created_at)}
              </div>
              <p className="text-sm whitespace-pre-wrap">{entry.body}</p>
            </div>
          ))}
        </div>
        <AddLogForm stepId={stepId} onCreated={invalidate} />
      </section>
    </div>
  );
}

function ItemCard({
  item,
  onMutate,
}: {
  item: ChecklistItem;
  onMutate: () => void;
}) {
  const [expanded, setExpanded] = useState(false);
  const [showAddOption, setShowAddOption] = useState(false);

  const statusMutation = useMutation({
    mutationFn: (status: ItemStatus) => api.updateItem(item.id, { status }),
    onSuccess: onMutate,
  });

  const selectMutation = useMutation({
    mutationFn: (optionId: number) =>
      api.updateItem(item.id, { selected_option_id: optionId }),
    onSuccess: onMutate,
  });

  const costMutation = useMutation({
    mutationFn: (actual_cost_cents: number) =>
      api.updateItem(item.id, { actual_cost_cents }),
    onSuccess: onMutate,
  });

  const selectedOption = item.options?.find(
    (o) => o.id === item.selected_option_id,
  );

  return (
    <div className="bg-gray-900 rounded-lg p-4">
      <div
        className="flex items-center justify-between cursor-pointer"
        onClick={() => setExpanded(!expanded)}
      >
        <div className="flex items-center gap-3">
          <span className="font-medium">{item.name}</span>
          <StatusBadge status={item.status} />
        </div>
        <div className="flex items-center gap-4 text-sm text-gray-400">
          {item.budget_cents != null && (
            <span>Budget: &euro;{formatCents(item.budget_cents)}</span>
          )}
          {item.actual_cost_cents != null && (
            <span>Paid: &euro;{formatCents(item.actual_cost_cents)}</span>
          )}
          {selectedOption && (
            <span className="text-indigo-400">
              Selected: {selectedOption.name}
            </span>
          )}
          <span className="text-xs">{expanded ? "▲" : "▼"}</span>
        </div>
      </div>

      {expanded && (
        <div className="mt-4 space-y-3">
          {/* Status controls */}
          <div className="flex gap-2">
            {(["researching", "ordered", "arrived"] as ItemStatus[]).map(
              (s) => (
                <button
                  key={s}
                  onClick={() => statusMutation.mutate(s)}
                  className={`px-2 py-1 rounded text-xs ${
                    item.status === s
                      ? "bg-indigo-600 text-white"
                      : "bg-gray-800 text-gray-400 hover:bg-gray-700"
                  }`}
                >
                  {s}
                </button>
              ),
            )}
          </div>

          {/* Actual cost input */}
          <div className="flex items-center gap-2">
            <span className="text-sm text-gray-400">Actual cost (cents):</span>
            <input
              type="number"
              defaultValue={item.actual_cost_cents ?? ""}
              onBlur={(e) => {
                const v = parseInt(e.target.value);
                if (!isNaN(v)) costMutation.mutate(v);
              }}
              className="w-32 bg-gray-800 rounded px-2 py-1 text-sm"
            />
          </div>

          {/* Options */}
          <div>
            <h4 className="text-sm font-medium text-gray-300 mb-2">Options</h4>
            {(item.options ?? []).length === 0 && !showAddOption && (
              <p className="text-xs text-gray-500">No options yet.</p>
            )}
            <div className="space-y-2">
              {(item.options ?? []).map((opt) => (
                <OptionRow
                  key={opt.id}
                  option={opt}
                  isSelected={opt.id === item.selected_option_id}
                  onSelect={() => selectMutation.mutate(opt.id)}
                  onMutate={onMutate}
                />
              ))}
            </div>
            {showAddOption ? (
              <AddOptionForm
                itemId={item.id}
                onCreated={() => {
                  setShowAddOption(false);
                  onMutate();
                }}
                onCancel={() => setShowAddOption(false)}
              />
            ) : (
              <button
                onClick={() => setShowAddOption(true)}
                className="mt-2 text-xs text-indigo-400 hover:text-indigo-300"
              >
                + Add option
              </button>
            )}
          </div>
        </div>
      )}
    </div>
  );
}

function OptionRow({
  option,
  isSelected,
  onSelect,
  onMutate,
}: {
  option: ItemOption;
  isSelected: boolean;
  onSelect: () => void;
  onMutate: () => void;
}) {
  const deleteMutation = useMutation({
    mutationFn: () => api.deleteOption(option.id),
    onSuccess: onMutate,
  });

  return (
    <div
      className={`flex items-center gap-3 p-2 rounded text-sm ${
        isSelected ? "bg-indigo-900/30 border border-indigo-700" : "bg-gray-800"
      }`}
    >
      <button
        onClick={onSelect}
        className={`w-4 h-4 rounded-full border-2 flex-shrink-0 ${
          isSelected
            ? "border-indigo-400 bg-indigo-400"
            : "border-gray-500 hover:border-indigo-400"
        }`}
      />
      <div className="flex-1 min-w-0">
        <span className="font-medium">{option.name}</span>
        {option.price_cents != null && (
          <span className="ml-2 text-gray-400">
            &euro;{formatCents(option.price_cents)}
          </span>
        )}
        {option.url && (
          <a
            href={option.url}
            target="_blank"
            rel="noopener noreferrer"
            className="ml-2 text-xs text-blue-400 hover:underline"
            onClick={(e) => e.stopPropagation()}
          >
            link
          </a>
        )}
        {option.notes && (
          <span className="ml-2 text-xs text-gray-500">{option.notes}</span>
        )}
      </div>
      <button
        onClick={() => deleteMutation.mutate()}
        className="text-xs text-red-400 hover:text-red-300"
      >
        x
      </button>
    </div>
  );
}

function AddOptionForm({
  itemId,
  onCreated,
  onCancel,
}: {
  itemId: number;
  onCreated: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [priceCents, setPriceCents] = useState("");
  const [notes, setNotes] = useState("");

  const mutation = useMutation({
    mutationFn: () =>
      api.createOption(itemId, {
        name,
        url: url || undefined,
        price_cents: priceCents ? parseInt(priceCents) : undefined,
        notes: notes || undefined,
      }),
    onSuccess: onCreated,
  });

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate();
      }}
      className="mt-2 space-y-2 bg-gray-800 rounded p-3"
    >
      <input
        type="text"
        placeholder="Option name"
        value={name}
        onChange={(e) => setName(e.target.value)}
        className="w-full bg-gray-700 rounded px-2 py-1 text-sm"
        autoFocus
      />
      <div className="flex gap-2">
        <input
          type="text"
          placeholder="URL (optional)"
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          className="flex-1 bg-gray-700 rounded px-2 py-1 text-sm"
        />
        <input
          type="number"
          placeholder="Price (cents)"
          value={priceCents}
          onChange={(e) => setPriceCents(e.target.value)}
          className="w-32 bg-gray-700 rounded px-2 py-1 text-sm"
        />
      </div>
      <input
        type="text"
        placeholder="Notes (optional)"
        value={notes}
        onChange={(e) => setNotes(e.target.value)}
        className="w-full bg-gray-700 rounded px-2 py-1 text-sm"
      />
      <div className="flex gap-2">
        <button
          type="submit"
          className="px-2 py-1 bg-indigo-600 hover:bg-indigo-500 rounded text-xs"
        >
          Add
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="px-2 py-1 bg-gray-700 hover:bg-gray-600 rounded text-xs"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function AddItemForm({
  stepId,
  onCreated,
}: {
  stepId: number;
  onCreated: () => void;
}) {
  const [show, setShow] = useState(false);
  const [name, setName] = useState("");
  const [budgetCents, setBudgetCents] = useState("");

  const mutation = useMutation({
    mutationFn: () =>
      api.createItem(stepId, {
        name,
        budget_cents: budgetCents ? parseInt(budgetCents) : undefined,
      }),
    onSuccess: () => {
      setName("");
      setBudgetCents("");
      setShow(false);
      onCreated();
    },
  });

  if (!show) {
    return (
      <button
        onClick={() => setShow(true)}
        className="mt-3 text-sm text-indigo-400 hover:text-indigo-300"
      >
        + Add item
      </button>
    );
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate();
      }}
      className="mt-3 bg-gray-900 rounded-lg p-4 space-y-2"
    >
      <div className="flex gap-2">
        <input
          type="text"
          placeholder="Item name"
          value={name}
          onChange={(e) => setName(e.target.value)}
          className="flex-1 bg-gray-800 rounded px-3 py-2 text-sm"
          autoFocus
        />
        <input
          type="number"
          placeholder="Budget (cents)"
          value={budgetCents}
          onChange={(e) => setBudgetCents(e.target.value)}
          className="w-40 bg-gray-800 rounded px-3 py-2 text-sm"
        />
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 rounded text-sm"
        >
          Add
        </button>
        <button
          type="button"
          onClick={() => setShow(false)}
          className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-sm"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function AddLogForm({
  stepId,
  onCreated,
}: {
  stepId: number;
  onCreated: () => void;
}) {
  const [show, setShow] = useState(false);
  const [body, setBody] = useState("");

  const mutation = useMutation({
    mutationFn: () => api.createLogEntry(stepId, { body }),
    onSuccess: () => {
      setBody("");
      setShow(false);
      onCreated();
    },
  });

  if (!show) {
    return (
      <button
        onClick={() => setShow(true)}
        className="mt-3 text-sm text-indigo-400 hover:text-indigo-300"
      >
        + Add log entry
      </button>
    );
  }

  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        mutation.mutate();
      }}
      className="mt-3 bg-gray-900 rounded-lg p-4 space-y-2"
    >
      <textarea
        placeholder="What did you do? What did you learn?"
        value={body}
        onChange={(e) => setBody(e.target.value)}
        className="w-full bg-gray-800 rounded px-3 py-2 text-sm"
        rows={4}
        autoFocus
      />
      <div className="flex gap-2">
        <button
          type="submit"
          className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 rounded text-sm"
        >
          Add
        </button>
        <button
          type="button"
          onClick={() => setShow(false)}
          className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-sm"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
