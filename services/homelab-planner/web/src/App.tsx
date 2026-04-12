import { Routes, Route } from "react-router-dom";
import Dashboard from "./pages/Dashboard";
import StepDetail from "./pages/StepDetail";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Dashboard />} />
      <Route path="/step/:id" element={<StepDetail />} />
    </Routes>
  );
}
