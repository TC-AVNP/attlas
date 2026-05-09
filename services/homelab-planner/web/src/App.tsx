import { Routes, Route } from "react-router-dom";
import Dashboard from "./pages/Dashboard";
import Steps from "./pages/Steps";
import StepDetail from "./pages/StepDetail";
import Schematic from "./pages/Schematic";
import Schematic3D from "./pages/Schematic3D";

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Dashboard />} />
      <Route path="/steps" element={<Steps />} />
      <Route path="/step/:id" element={<StepDetail />} />
      <Route path="/schematic" element={<Schematic />} />
      <Route path="/schematic-3d" element={<Schematic3D />} />
    </Routes>
  );
}
