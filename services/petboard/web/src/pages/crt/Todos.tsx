import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import { formatRelative } from "../../lib/format";

export default function Todos() {
  const queryClient = useQueryClient();
  const [includeDone, setIncludeDone] = useState(false);
  const [draft, setDraft] = useState("");

  const { data, isLoading, error } = useQuery({
    queryKey: ["todos", includeDone],
    queryFn: () => api.listTodos(includeDone),
  });

  const invalidate = () => queryClient.invalidateQueries({ queryKey: ["todos"] });

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
    <div style={{
      padding: '20px 32px',
      fontFamily: 'var(--font-mono)',
      fontSize: 13,
      height: '100%',
      overflow: 'auto',
    }}>
      {/* Status line */}
      <div style={{
        display: 'flex', justifyContent: 'space-between',
        marginBottom: 18, paddingBottom: 6,
        borderBottom: '1px solid var(--phos-fg-faint)',
        fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.1em',
      }}>
        <span className="fg-dim">
          <Link to="/" style={{ color: 'inherit', textDecoration: 'none' }}>PETBOARD</Link>
          {' > '}/todos
        </span>
        <span className="fg-dim">ESC BACK</span>
      </div>

      <div className="h-bitmap-lg fg-bright" style={{ letterSpacing: '0.08em', marginBottom: 6 }}>
        T O D O S
      </div>
      <p className="fg-dim" style={{ fontSize: 11, marginBottom: 20 }}>
        // standalone reminders that aren't tied to any project
      </p>

      {/* Add form */}
      <form onSubmit={(e) => {
        e.preventDefault();
        const text = draft.trim();
        if (!text) return;
        create.mutate(text, { onSuccess: () => setDraft("") });
      }} style={{ display: 'flex', gap: 8, marginBottom: 18 }}>
        <input
          type="text"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="what should you not forget?"
          className="crt-input"
          style={{ flex: 1 }}
        />
        <button type="submit" className="btn" disabled={!draft.trim() || create.isPending}>
          ADD
        </button>
      </form>

      {/* Toggle done */}
      <div style={{ marginBottom: 14, fontSize: 11 }}>
        <span
          data-click="1"
          onClick={() => setIncludeDone(!includeDone)}
          style={{ cursor: 'pointer', color: 'var(--phos-fg-dim)' }}
        >
          [{includeDone ? 'X' : ' '}] SHOW COMPLETED
        </span>
      </div>

      {/* List */}
      {isLoading && <div className="fg-dim">Loading<span className="cursor-inline" /></div>}
      {error && (
        <div style={{ color: 'var(--danger)' }}>
          Failed to load todos: {(error as Error).message}
        </div>
      )}
      {data && data.todos.length === 0 && (
        <div className="fg-faint" style={{ fontStyle: 'italic', marginTop: 12 }}>
          (nothing to remember. enjoy the silence.)
        </div>
      )}
      {data && data.todos.length > 0 && (
        <div>
          {data.todos.map((t) => {
            const done = t.completed_at != null;
            return (
              <div key={t.id} style={{
                display: 'flex', alignItems: 'flex-start', gap: 10,
                padding: '6px 0',
                borderBottom: '1px dotted var(--phos-fg-faint)',
              }}>
                <span
                  data-click="1"
                  onClick={() => update.mutate({ id: t.id, body: { completed: !done } })}
                  className="tt-mono"
                  style={{ cursor: 'pointer', flex: '0 0 auto', color: done ? 'var(--phos-fg-dim)' : 'var(--phos-fg)' }}
                >
                  {done ? '[\u2713]' : '[ ]'}
                </span>
                <span style={{
                  flex: 1,
                  color: done ? 'var(--phos-fg-faint)' : 'var(--phos-fg)',
                  textDecoration: done ? 'line-through' : 'none',
                }}>
                  {t.text}
                </span>
                <span className="fg-faint" style={{ fontSize: 10 }}>
                  {formatRelative(t.created_at)}
                </span>
                <span
                  data-click="1"
                  onClick={() => { if (confirm(`Delete "${t.text}"?`)) remove.mutate(t.id); }}
                  style={{ cursor: 'pointer', color: 'var(--danger)', opacity: 0.6, fontSize: 11 }}
                  title="delete"
                >
                  x
                </span>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
