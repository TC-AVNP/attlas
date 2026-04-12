import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";

export default function Groups() {
  const qc = useQueryClient();
  const { data: groups, isLoading } = useQuery({
    queryKey: ["groups"],
    queryFn: api.listGroups,
  });
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: api.me });

  const [showCreate, setShowCreate] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");

  const createMut = useMutation({
    mutationFn: () => api.createGroup(name, description, ""),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["groups"] });
      setShowCreate(false);
      setName("");
      setDescription("");
    },
  });

  if (isLoading) return <p className="text-gray-400">Loading groups...</p>;

  return (
    <div>
      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Groups</h1>
        {me?.is_admin && (
          <button
            onClick={() => setShowCreate(!showCreate)}
            className="bg-brand text-white px-4 py-2 rounded-lg text-sm font-medium hover:bg-brand/90"
          >
            New Group
          </button>
        )}
      </div>

      {showCreate && (
        <div className="bg-white rounded-lg shadow-sm border p-4 mb-4">
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Group name"
            className="w-full border rounded px-3 py-2 mb-2 text-sm"
          />
          <input
            type="text"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Description (optional)"
            className="w-full border rounded px-3 py-2 mb-3 text-sm"
          />
          <button
            onClick={() => createMut.mutate()}
            disabled={!name.trim() || createMut.isPending}
            className="bg-brand text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
          >
            {createMut.isPending ? "Creating..." : "Create"}
          </button>
        </div>
      )}

      {!groups?.length ? (
        <p className="text-gray-400 text-center py-8">No groups yet.</p>
      ) : (
        <div className="space-y-2">
          {groups.map((g) => (
            <Link
              key={g.id}
              to={`/groups/${g.id}`}
              className="block bg-white rounded-lg shadow-sm border p-4 hover:border-brand/30"
            >
              <div className="flex items-center justify-between">
                <div>
                  <h3 className="font-semibold">{g.name}</h3>
                  {g.description && (
                    <p className="text-sm text-gray-500">{g.description}</p>
                  )}
                </div>
                <span className="text-sm text-gray-400">
                  {g.members.length} member{g.members.length !== 1 && "s"}
                </span>
              </div>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
