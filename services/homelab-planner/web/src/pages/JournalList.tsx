import { Link } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState } from "react";

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("en-GB", { day: "numeric", month: "short" });
}

function formatYear(dateStr: string): string {
  return dateStr.slice(0, 4);
}

export default function JournalList() {
  const queryClient = useQueryClient();
  const [showForm, setShowForm] = useState(false);
  const [date, setDate] = useState(() => new Date().toISOString().split("T")[0]);
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["journal"],
    queryFn: api.listJournal,
  });

  const createMutation = useMutation({
    mutationFn: () => api.createJournalEntry({ date, title, body }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["journal"] });
      setShowForm(false);
      setTitle("");
      setBody("");
    },
  });

  const entries = data?.entries ?? [];

  return (
    <div>
      {/* Header */}
      <div className="mb-8">
        <div className="flex items-start justify-between">
          <div>
            <h1 className="text-[#ff8700] text-[24px] font-bold tracking-tight" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              Build Journal
            </h1>
            <p className="text-[#555] text-[13px] mt-1">Day-by-day progress on the homelab build</p>
          </div>
          <button
            onClick={() => setShowForm(!showForm)}
            className="shrink-0 px-3 py-1.5 bg-[#00ff00] text-black rounded-md text-[12px] font-semibold hover:bg-[#00dd00] transition-colors"
            style={{ fontFamily: "'JetBrains Mono', monospace" }}
          >
            {showForm ? "Cancel" : "+ New"}
          </button>
        </div>
        <div className="h-px bg-[#222] mt-4" />
      </div>

      {/* Create form */}
      {showForm && (
        <div className="bg-[#111] border border-[#222] rounded-md p-4 mb-6 flex flex-col gap-3">
          <div className="flex gap-3">
            <input
              type="date"
              value={date}
              onChange={(e) => setDate(e.target.value)}
              className="bg-[#0c0c0c] border border-[#222] rounded-md px-3 py-1.5 text-[13px] text-[#c0c0c0] focus:outline-none focus:border-[#333]"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            />
            <input
              type="text"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Entry title..."
              className="flex-1 bg-[#0c0c0c] border border-[#222] rounded-md px-3 py-1.5 text-[13px] text-[#c0c0c0] placeholder-[#444] focus:outline-none focus:border-[#333]"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            />
          </div>
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            placeholder="What did you work on today? (Markdown)"
            rows={8}
            className="w-full bg-[#0c0c0c] border border-[#222] rounded-md p-3 text-[13px] text-[#c0c0c0] placeholder-[#444] resize-none focus:outline-none focus:border-[#333]"
            style={{ fontFamily: "'JetBrains Mono', monospace" }}
          />
          <button
            onClick={() => createMutation.mutate()}
            disabled={!title.trim() || createMutation.isPending}
            className="self-end px-4 py-1.5 bg-[#00ff00] text-black rounded-md text-[12px] font-semibold hover:bg-[#00dd00] disabled:opacity-40 transition-colors"
            style={{ fontFamily: "'JetBrains Mono', monospace" }}
          >
            {createMutation.isPending ? "Saving..." : "Publish"}
          </button>
        </div>
      )}

      {/* Entries */}
      {isLoading ? (
        <div className="text-[#555] text-sm pt-8 text-center" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          Loading...
        </div>
      ) : entries.length === 0 ? (
        <div className="text-[#444] text-sm text-center py-16" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          No entries yet
        </div>
      ) : (
        <div className="flex flex-col">
          {entries.map((entry, i) => {
            const showYear = i === 0 || formatYear(entry.date) !== formatYear(entries[i - 1].date);
            return (
              <div key={entry.id}>
                {showYear && (
                  <div className="text-[11px] text-[#444] mt-4 mb-2" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
                    {formatYear(entry.date)}
                  </div>
                )}
                <Link
                  to={`/journal/${entry.id}`}
                  className="group flex items-baseline gap-4 px-3 py-2.5 -mx-3 rounded-md hover:bg-[#151515] transition-colors"
                >
                  <span className="text-[#444] text-[12px] shrink-0 w-14" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
                    {formatDate(entry.date)}
                  </span>
                  <span className="text-[#999] text-[14px] group-hover:text-[#c0c0c0] transition-colors">
                    {entry.title}
                  </span>
                </Link>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
