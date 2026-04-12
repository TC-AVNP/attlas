import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { useState } from "react";
import { api } from "../api/client";
import { formatCents } from "../lib/format";

export default function Dashboard() {
  const queryClient = useQueryClient();
  const [showForm, setShowForm] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["steps"],
    queryFn: api.listSteps,
  });

  const createMutation = useMutation({
    mutationFn: () => api.createStep({ title, description }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["steps"] });
      setTitle("");
      setDescription("");
      setShowForm(false);
    },
  });

  const steps = data?.steps ?? [];

  const totalBudget = steps.reduce((sum, s) => sum + s.budget_cents, 0);
  const totalActual = steps.reduce((sum, s) => sum + s.actual_cents, 0);

  if (isLoading) {
    return <div className="p-8 text-gray-500">Loading...</div>;
  }

  return (
    <div className="max-w-4xl mx-auto p-6">
      <div className="flex items-center justify-between mb-8">
        <h1 className="text-2xl font-bold">Homelab Planner</h1>
        <button
          onClick={() => setShowForm(!showForm)}
          className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 rounded text-sm"
        >
          + Add Step
        </button>
      </div>

      {/* Cost summary */}
      <div className="grid grid-cols-2 gap-4 mb-8">
        <div className="bg-gray-900 rounded-lg p-4">
          <div className="text-sm text-gray-400">Total Budget</div>
          <div className="text-xl font-bold text-yellow-400">
            &euro;{formatCents(totalBudget)}
          </div>
        </div>
        <div className="bg-gray-900 rounded-lg p-4">
          <div className="text-sm text-gray-400">Total Spent</div>
          <div className="text-xl font-bold text-green-400">
            &euro;{formatCents(totalActual)}
          </div>
        </div>
      </div>

      {showForm && (
        <form
          onSubmit={(e) => {
            e.preventDefault();
            createMutation.mutate();
          }}
          className="bg-gray-900 rounded-lg p-4 mb-6 space-y-3"
        >
          <input
            type="text"
            placeholder="Step title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            className="w-full bg-gray-800 rounded px-3 py-2 text-sm"
            autoFocus
          />
          <textarea
            placeholder="Description (optional)"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            className="w-full bg-gray-800 rounded px-3 py-2 text-sm"
            rows={2}
          />
          <div className="flex gap-2">
            <button
              type="submit"
              className="px-3 py-1.5 bg-indigo-600 hover:bg-indigo-500 rounded text-sm"
            >
              Create
            </button>
            <button
              type="button"
              onClick={() => setShowForm(false)}
              className="px-3 py-1.5 bg-gray-700 hover:bg-gray-600 rounded text-sm"
            >
              Cancel
            </button>
          </div>
        </form>
      )}

      {/* Steps list */}
      <div className="space-y-3">
        {steps.map((step) => (
          <Link
            key={step.id}
            to={`/step/${step.id}`}
            className="block bg-gray-900 rounded-lg p-4 hover:bg-gray-800 transition-colors"
          >
            <div className="flex items-center justify-between mb-1">
              <h2 className="font-semibold">
                {step.completed_at ? (
                  <span className="text-green-400 mr-2">[done]</span>
                ) : null}
                {step.title}
              </h2>
              <span className="text-sm text-gray-400">
                {step.arrived_count}/{step.item_count} items
              </span>
            </div>
            {step.description && (
              <p className="text-sm text-gray-400 mb-2">{step.description}</p>
            )}
            <div className="flex gap-6 text-xs text-gray-500">
              <span>
                Budget: &euro;
                {formatCents(
                  step.total_budget_cents ?? step.budget_cents,
                )}
              </span>
              <span>
                Spent: &euro;{formatCents(step.actual_cents)}
              </span>
              {/* Progress bar */}
              {step.item_count > 0 && (
                <div className="flex-1 flex items-center gap-2">
                  <div className="flex-1 bg-gray-800 rounded-full h-1.5">
                    <div
                      className="bg-green-500 rounded-full h-1.5 transition-all"
                      style={{
                        width: `${(step.arrived_count / step.item_count) * 100}%`,
                      }}
                    />
                  </div>
                </div>
              )}
            </div>
          </Link>
        ))}

        {steps.length === 0 && (
          <p className="text-gray-500 text-center py-12">
            No steps yet. Add your first milestone to get started.
          </p>
        )}
      </div>
    </div>
  );
}
