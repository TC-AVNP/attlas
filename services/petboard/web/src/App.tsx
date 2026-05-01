// App owns routing only. Each route maps to a page component under
// src/pages/. The basename is /petboard so all <Link to="/p/foo">
// resolve to /petboard/p/foo, matching how Caddy serves the SPA.
//
// useLiveUpdates() subscribes to /petboard/api/events and invalidates
// react-query caches as mutations land — so the canvas animates when
// Claude Code is editing via MCP from another client.

import { Routes, Route } from "react-router-dom";
import Kanban from "./pages/Kanban";
import Universe from "./pages/Universe";
import ProjectDetail from "./pages/ProjectDetail";
import Todos from "./pages/Todos";
import { useLiveUpdates } from "./api/events";

export default function App() {
  useLiveUpdates();
  return (
    <Routes>
      <Route path="/" element={<Kanban />} />
      <Route path="/universe" element={<Universe />} />
      <Route path="/p/:slug" element={<ProjectDetail />} />
      <Route path="/todos" element={<Todos />} />
    </Routes>
  );
}
