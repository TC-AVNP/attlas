// Todos is the standalone "things to do that aren't a project" view at
// /petboard/todos. Reachable from the Universe header. Backed by the
// /api/todos endpoints.
//
// Intentionally tiny: a single text input for adding, a list with
// click-to-toggle checkbox and click-to-delete X. No edit-in-place, no
// priority, no due dates — if a todo grows enough to need any of that,
// it should become a real project via "add project" instead.

import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import { formatRelative } from "../lib/format";

export default function Todos() {
  const queryClient = useQueryClient();
  const [includeDone, setIncludeDone] = useState(false);
  const [draft, setDraft] = useState("");

  const { data, isLoading, error } = useQuery({
    queryKey: ["todos", includeDone],
    queryFn: () => api.listTodos(includeDone),
  });

  const invalidate = () => {
    queryClient.invalidateQueries({ queryKey: ["todos"] });
  };

  const create = useMutation({
    mutationFn: (text: string) => api.createTodo(text),
    onSuccess: invalidate,
  });
  const update = useMutation({
    mutationFn: ({ id, body }: { id: number; body: { text?: string; completed?: boolean } }) =>
      api.updateTodo(id, body),
    onSuccess: invalidate,
  });
  const remove = useMutation({
    mutationFn: (id: number) => api.deleteTodo(id),
    onSuccess: invalidate,
  });

  return (
    <main className="min-h-screen bg-neutral-950 text-neutral-100 p-8">
      <div className="max-w-2xl mx-auto">
        <Link to="/" className="text-sm text-neutral-400 hover:text-neutral-200">
          ← back to universe
        </Link>

        <header className="mt-4">
          <h1 className="text-2xl font-semibold tracking-tight">todos</h1>
          <p className="mt-1 text-sm text-neutral-500">
            standalone reminders that aren't tied to any project — refactors,
            chores, "I should think about this someday" stuff.
          </p>
        </header>

        {/* Add form */}
        <form
          onSubmit={(e) => {
            e.preventDefault();
            const text = draft.trim();
            if (!text) return;
            create.mutate(text, { onSuccess: () => setDraft("") });
          }}
          className="mt-6 flex gap-2"
        >
          <input
            type="text"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            placeholder="what should you not forget?"
            className="flex-1 bg-neutral-900 border border-neutral-700 rounded px-3 py-2 text-sm focus:border-neutral-500 focus:outline-none"
          />
          <button
            type="submit"
            disabled={!draft.trim() || create.isPending}
            className="px-4 py-2 text-sm rounded border border-neutral-700 bg-neutral-900 hover:bg-neutral-800 disabled:opacity-40"
          >
            add
          </button>
        </form>

        {/* Toggle done */}
        <div className="mt-6 flex items-center gap-3 text-xs text-neutral-500">
          <label className="flex items-center gap-2 cursor-pointer">
            <input
              type="checkbox"
              checked={includeDone}
              onChange={(e) => setIncludeDone(e.target.checked)}
              className="accent-neutral-500"
            />
            show completed
          </label>
        </div>

        {/* List */}
        {isLoading && <p className="mt-6 text-neutral-500 text-sm">loading…</p>}
        {error && (
          <div className="mt-6 rounded border border-red-500/40 bg-red-500/10 p-3 text-red-300 text-sm">
            failed to load todos: {(error as Error).message}
          </div>
        )}
        {data && data.todos.length === 0 && (
          <p className="mt-6 text-neutral-500 text-sm italic">
            nothing to remember. enjoy the silence.
          </p>
        )}
        {data && data.todos.length > 0 && (
          <ul className="mt-4 space-y-1">
            {data.todos.map((t) => {
              const done = t.completed_at != null;
              return (
                <li
                  key={t.id}
                  className="group flex items-center gap-3 rounded border border-neutral-900 bg-neutral-900/30 px-3 py-2 hover:border-neutral-800"
                >
                  <button
                    type="button"
                    onClick={() =>
                      update.mutate({ id: t.id, body: { completed: !done } })
                    }
                    className={`h-4 w-4 rounded border flex-shrink-0 flex items-center justify-center text-[10px] ${
                      done
                        ? "border-emerald-500/60 bg-emerald-500/20 text-emerald-300"
                        : "border-neutral-600 hover:border-neutral-400"
                    }`}
                    aria-label={done ? "mark not done" : "mark done"}
                  >
                    {done ? "✓" : ""}
                  </button>
                  <span
                    className={`flex-1 text-sm ${
                      done
                        ? "text-neutral-500 line-through"
                        : "text-neutral-200"
                    }`}
                  >
                    {t.text}
                  </span>
                  <span className="text-xs text-neutral-600 tabular-nums">
                    {formatRelative(t.created_at)}
                  </span>
                  <button
                    type="button"
                    onClick={() => {
                      if (confirm(`Delete "${t.text}"?`)) remove.mutate(t.id);
                    }}
                    className="opacity-0 group-hover:opacity-100 text-neutral-600 hover:text-red-400 transition-opacity text-sm"
                    aria-label="delete"
                  >
                    ×
                  </button>
                </li>
              );
            })}
          </ul>
        )}
      </div>
    </main>
  );
}
