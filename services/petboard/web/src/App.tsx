// App owns routing only. Each route maps to a page component under
// src/pages/. The basename is /petboard so all <Link to="/p/foo">
// resolve to /petboard/p/foo, matching how Caddy serves the SPA.

import { Routes, Route } from "react-router-dom";
import Universe from "./pages/Universe";
import ProjectDetail from "./pages/ProjectDetail";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Universe />} />
      <Route path="/p/:slug" element={<ProjectDetail />} />
    </Routes>
  );
}
