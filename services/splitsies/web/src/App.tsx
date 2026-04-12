import { Routes, Route, Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "./api/client";
import { useLiveUpdates } from "./api/events";
import Layout from "./components/Layout";
import Login from "./pages/Login";
import AccessDenied from "./pages/AccessDenied";
import Home from "./pages/Home";
import Groups from "./pages/Groups";
import GroupDetail from "./pages/GroupDetail";
import Admin from "./pages/Admin";
import Overview from "./pages/Overview";

export default function App() {
  const { data: user, isLoading, error } = useQuery({
    queryKey: ["me"],
    queryFn: api.me,
    retry: false,
  });

  useLiveUpdates();

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-screen">
        <div className="text-gray-400 text-lg">Loading...</div>
      </div>
    );
  }

  // Not logged in
  if (error || !user) {
    return (
      <Routes>
        <Route path="/access-denied" element={<AccessDenied />} />
        <Route path="*" element={<Login />} />
      </Routes>
    );
  }

  return (
    <Layout user={user}>
      <Routes>
        <Route path="/" element={<Home />} />
        <Route path="/groups" element={<Groups />} />
        <Route path="/groups/:id" element={<GroupDetail />} />
        <Route path="/overview" element={<Overview />} />
        {user.is_admin && <Route path="/admin" element={<Admin />} />}
        <Route path="/access-denied" element={<Navigate to="/" />} />
        <Route path="*" element={<Navigate to="/" />} />
      </Routes>
    </Layout>
  );
}
