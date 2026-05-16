import { NavLink, Outlet, useLocation } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

export default function WikiLayout() {
  const { data } = useQuery({
    queryKey: ["pages"],
    queryFn: api.listPages,
  });
  const location = useLocation();

  const pages = data?.pages ?? [];
  const isJournalActive = location.pathname.startsWith("/journal");

  return (
    <div className="min-h-screen flex">
      {/* Sidebar */}
      <aside className="w-56 shrink-0 border-r border-[#222] bg-[#111] flex flex-col sticky top-0 h-screen overflow-y-auto">
        {/* Logo area */}
        <div className="px-5 py-5 border-b border-[#222]">
          <NavLink to="/wiki/home" className="block">
            <span className="text-[#ff8700] font-bold text-base tracking-tight" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              homelab
            </span>
            <span className="text-[#555] text-xs block mt-0.5" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
              wiki
            </span>
          </NavLink>
        </div>

        {/* Navigation */}
        <nav className="flex-1 px-3 py-4 flex flex-col gap-0.5">
          {pages.map((p) => (
            <NavLink
              key={p.slug}
              to={`/wiki/${p.slug}`}
              className={({ isActive }) =>
                `px-3 py-1.5 rounded-md text-[13px] transition-colors ${
                  isActive
                    ? "bg-[#00ff00]/10 text-[#00ff00]"
                    : "text-[#999] hover:bg-[#1a1a1a] hover:text-[#c0c0c0]"
                }`
              }
            >
              {p.title}
            </NavLink>
          ))}

          <div className="h-px bg-[#222] my-3" />

          <NavLink
            to="/journal"
            className={() =>
              `px-3 py-1.5 rounded-md text-[13px] transition-colors flex items-center gap-2 ${
                isJournalActive
                  ? "bg-[#00ff00]/10 text-[#00ff00]"
                  : "text-[#999] hover:bg-[#1a1a1a] hover:text-[#c0c0c0]"
              }`
            }
          >
            <span className="text-[10px]">&#9998;</span>
            Build Journal
          </NavLink>
        </nav>

        {/* Footer */}
        <div className="px-5 py-3 border-t border-[#222] text-[10px] text-[#555]" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
          attlas.uk
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 min-w-0 flex justify-center">
        <div className="w-full max-w-3xl px-10 py-8">
          <Outlet />
        </div>
      </main>
    </div>
  );
}
