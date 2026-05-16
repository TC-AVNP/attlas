import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import Markdown from "../components/Markdown";
import { useState } from "react";

function formatDate(dateStr: string): string {
  const d = new Date(dateStr + "T00:00:00");
  return d.toLocaleDateString("en-GB", {
    weekday: "long",
    day: "numeric",
    month: "long",
    year: "numeric",
  });
}

export default function JournalEntryPage() {
  const { id } = useParams<{ id: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [editTitle, setEditTitle] = useState("");
  const [editBody, setEditBody] = useState("");

  const entryId = Number(id);

  const { data: entry, isLoading } = useQuery({
    queryKey: ["journal", entryId],
    queryFn: () => api.getJournalEntry(entryId),
    enabled: !isNaN(entryId),
  });

  const saveMutation = useMutation({
    mutationFn: () => api.updateJournalEntry(entryId, { title: editTitle, body: editBody }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["journal", entryId] });
      setEditing(false);
    },
  });

  const deleteMutation = useMutation({
    mutationFn: () => api.deleteJournalEntry(entryId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["journal"] });
      navigate("/journal");
    },
  });

  if (isLoading) {
    return (
      <div className="text-[#555] text-sm pt-12 text-center" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
        Loading...
      </div>
    );
  }

  if (!entry) {
    return (
      <div className="pt-12 text-center">
        <p className="text-[#555] text-sm mb-4" style={{ fontFamily: "'JetBrains Mono', monospace" }}>Entry not found</p>
        <button
          onClick={() => navigate("/journal")}
          className="text-[#00ff00] text-sm hover:underline"
          style={{ fontFamily: "'JetBrains Mono', monospace" }}
        >
          Back to journal
        </button>
      </div>
    );
  }

  if (editing) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h1 className="text-[#ff8700] text-lg font-semibold" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
            Editing entry
          </h1>
          <div className="flex gap-2">
            <button
              onClick={() => setEditing(false)}
              className="px-3 py-1.5 border border-[#333] rounded-md text-[13px] text-[#999] hover:text-[#c0c0c0] hover:bg-[#1a1a1a] transition-colors"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            >
              Cancel
            </button>
            <button
              onClick={() => saveMutation.mutate()}
              disabled={saveMutation.isPending}
              className="px-3 py-1.5 bg-[#00ff00] text-black rounded-md text-[13px] font-semibold hover:bg-[#00dd00] disabled:opacity-50 transition-colors"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            >
              {saveMutation.isPending ? "Saving..." : "Save"}
            </button>
          </div>
        </div>
        <input
          type="text"
          value={editTitle}
          onChange={(e) => setEditTitle(e.target.value)}
          className="bg-[#111] border border-[#222] rounded-md px-3 py-2 text-[14px] text-[#c0c0c0] focus:outline-none focus:border-[#333]"
          style={{ fontFamily: "'JetBrains Mono', monospace" }}
        />
        <textarea
          value={editBody}
          onChange={(e) => setEditBody(e.target.value)}
          className="w-full h-[65vh] bg-[#111] border border-[#222] rounded-md p-4 text-[#c0c0c0] text-[14px] resize-none focus:outline-none focus:border-[#333]"
          style={{ fontFamily: "'JetBrains Mono', monospace" }}
        />
      </div>
    );
  }

  return (
    <div>
      {/* Back link */}
      <button
        onClick={() => navigate("/journal")}
        className="text-[11px] text-[#444] hover:text-[#999] transition-colors mb-6 block"
        style={{ fontFamily: "'JetBrains Mono', monospace" }}
      >
        &larr; journal
      </button>

      {/* Header */}
      <div className="mb-8">
        <div className="text-[12px] text-[#555] mb-2" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          {formatDate(entry.date)}
        </div>
        <div className="flex items-start justify-between">
          <h1 className="text-[#ff8700] text-[24px] font-bold tracking-tight" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
            {entry.title}
          </h1>
          <div className="flex gap-1.5 shrink-0 mt-1">
            <button
              onClick={() => {
                setEditTitle(entry.title);
                setEditBody(entry.body);
                setEditing(true);
              }}
              className="px-2.5 py-1 rounded-md text-[11px] text-[#555] hover:text-[#999] hover:bg-[#1a1a1a] transition-colors"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            >
              edit
            </button>
            <button
              onClick={() => {
                if (confirm("Delete this entry?")) deleteMutation.mutate();
              }}
              className="px-2.5 py-1 rounded-md text-[11px] text-[#555] hover:text-[#ff5555] hover:bg-[#1a1a1a] transition-colors"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            >
              delete
            </button>
          </div>
        </div>
        <div className="h-px bg-[#222] mt-4" />
      </div>

      <Markdown content={entry.body} />
    </div>
  );
}
