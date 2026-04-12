import { useQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../api/client";

export default function Admin() {
  const qc = useQueryClient();
  const { data: users, isLoading } = useQuery({
    queryKey: ["users"],
    queryFn: api.listUsers,
  });

  const [email, setEmail] = useState("");
  const [isAdmin, setIsAdmin] = useState(false);

  const addMut = useMutation({
    mutationFn: () => api.addUser(email, isAdmin),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setEmail("");
      setIsAdmin(false);
    },
  });

  const removeMut = useMutation({
    mutationFn: (id: number) => api.removeUser(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  if (isLoading) return <p className="text-gray-400">Loading...</p>;

  return (
    <div>
      <h1 className="text-2xl font-bold mb-6">User Management</h1>

      {/* Add user form */}
      <div className="bg-white rounded-lg shadow-sm border p-4 mb-6">
        <h2 className="text-sm font-semibold mb-3">Add User</h2>
        <div className="flex gap-2 items-end">
          <div className="flex-1">
            <label className="block text-xs text-gray-500 mb-1">
              Google Email
            </label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              placeholder="user@gmail.com"
              className="w-full border rounded px-3 py-2 text-sm"
            />
          </div>
          <label className="flex items-center gap-2 text-sm pb-2">
            <input
              type="checkbox"
              checked={isAdmin}
              onChange={(e) => setIsAdmin(e.target.checked)}
            />
            Admin
          </label>
          <button
            onClick={() => addMut.mutate()}
            disabled={!email.trim() || addMut.isPending}
            className="bg-brand text-white px-4 py-2 rounded text-sm font-medium disabled:opacity-50"
          >
            {addMut.isPending ? "Adding..." : "Add"}
          </button>
        </div>
        {addMut.isError && (
          <p className="text-red-500 text-sm mt-2">
            {(addMut.error as Error).message}
          </p>
        )}
      </div>

      {/* User list */}
      <div className="space-y-2">
        {users?.map((u) => (
          <div
            key={u.id}
            className={`bg-white rounded-lg shadow-sm border p-4 flex items-center justify-between ${
              !u.is_active ? "opacity-50" : ""
            }`}
          >
            <div className="flex items-center gap-3">
              {u.picture && (
                <img
                  src={u.picture}
                  alt=""
                  className="w-8 h-8 rounded-full"
                />
              )}
              <div>
                <p className="font-medium text-sm">
                  {u.name || u.email}
                  {u.is_admin && (
                    <span className="ml-2 text-xs text-brand bg-brand/10 px-2 py-0.5 rounded">
                      admin
                    </span>
                  )}
                  {!u.is_active && (
                    <span className="ml-2 text-xs text-red-500 bg-red-50 px-2 py-0.5 rounded">
                      removed
                    </span>
                  )}
                </p>
                <p className="text-xs text-gray-400">{u.email}</p>
              </div>
            </div>
            {u.is_active && (
              <button
                onClick={() => {
                  if (
                    confirm(
                      `Remove ${u.name || u.email}? They will lose access but remain in expense history.`,
                    )
                  )
                    removeMut.mutate(u.id);
                }}
                className="text-red-400 hover:text-red-600 text-xs"
              >
                Remove
              </button>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
