import { Routes, Route, Navigate } from "react-router-dom";
import WikiLayout from "./layouts/WikiLayout";
import WikiPage from "./pages/WikiPage";
import JournalList from "./pages/JournalList";
import JournalEntryPage from "./pages/JournalEntryPage";

export default function App() {
  return (
    <Routes>
      <Route element={<WikiLayout />}>
        <Route path="/" element={<Navigate to="/wiki/home" replace />} />
        <Route path="/wiki/:slug" element={<WikiPage />} />
        <Route path="/journal" element={<JournalList />} />
        <Route path="/journal/:id" element={<JournalEntryPage />} />
      </Route>
    </Routes>
  );
}
