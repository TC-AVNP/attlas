import { useParams, useNavigate } from "react-router-dom";
import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api/client";
import Markdown from "../components/Markdown";
import Schematic3DEmbed from "../components/Schematic3DEmbed";
import SchematicEmbed from "../components/SchematicEmbed";
import { useState } from "react";

function formatDate(ts: number): string {
  return new Date(ts * 1000).toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  });
}

export default function WikiPage() {
  const { slug } = useParams<{ slug: string }>();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [editing, setEditing] = useState(false);
  const [editBody, setEditBody] = useState("");

  const { data: page, isLoading, error } = useQuery({
    queryKey: ["page", slug],
    queryFn: () => api.getPage(slug!),
    enabled: !!slug,
  });

  const saveMutation = useMutation({
    mutationFn: (body: string) => api.updatePage(slug!, { body }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["page", slug] });
      setEditing(false);
    },
  });

  if (isLoading) {
    return (
      <div className="text-[#555] text-sm pt-12 text-center" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
        Loading...
      </div>
    );
  }

  if (error || !page) {
    return (
      <div className="pt-12 text-center">
        <p className="text-[#555] text-sm mb-4" style={{ fontFamily: "'JetBrains Mono', monospace" }}>Page not found</p>
        <button
          onClick={() => navigate("/wiki/home")}
          className="text-[#00ff00] text-sm hover:underline"
          style={{ fontFamily: "'JetBrains Mono', monospace" }}
        >
          Go home
        </button>
      </div>
    );
  }

  if (editing) {
    return (
      <div className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h1 className="text-[#ff8700] text-lg font-semibold" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
            Editing: {page.title}
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
              onClick={() => saveMutation.mutate(editBody)}
              disabled={saveMutation.isPending}
              className="px-3 py-1.5 bg-[#00ff00] text-black rounded-md text-[13px] font-semibold hover:bg-[#00dd00] disabled:opacity-50 transition-colors"
              style={{ fontFamily: "'JetBrains Mono', monospace" }}
            >
              {saveMutation.isPending ? "Saving..." : "Save"}
            </button>
          </div>
        </div>
        <textarea
          value={editBody}
          onChange={(e) => setEditBody(e.target.value)}
          className="w-full h-[75vh] bg-[#111] border border-[#222] rounded-md p-4 text-[#c0c0c0] text-[14px] resize-none focus:outline-none focus:border-[#333]"
          style={{ fontFamily: "'JetBrains Mono', monospace" }}
        />
      </div>
    );
  }

  return (
    <div>
      {/* Page header */}
      <div className="mb-8">
        <div className="flex items-start justify-between">
          <h1 className="text-[#ff8700] text-[24px] font-bold tracking-tight" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
            {page.title}
          </h1>
          <button
            onClick={() => {
              setEditBody(page.body);
              setEditing(true);
            }}
            className="shrink-0 mt-1 px-2.5 py-1 rounded-md text-[11px] text-[#555] hover:text-[#999] hover:bg-[#1a1a1a] transition-colors"
            style={{ fontFamily: "'JetBrains Mono', monospace" }}
          >
            edit
          </button>
        </div>
        <div className="text-[11px] text-[#444] mt-1.5" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          Last updated {formatDate(page.updated_at)}
        </div>
        <div className="h-px bg-[#222] mt-4" />
      </div>

      {/* Embed 3D model for the standard cluster page */}
      {slug === "standard-cluster" && <Schematic3DEmbed />}

      {/* Embed 2D schematic for the networking page */}
      {slug === "networking" && <SchematicEmbed />}

      <Markdown content={page.body} />
    </div>
  );
}
