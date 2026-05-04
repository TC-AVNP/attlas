import { useState, useEffect, lazy, Suspense } from "react";
import { Routes, Route } from "react-router-dom";
import Kanban from "./pages/Kanban";
import ProjectDetail from "./pages/ProjectDetail";
import Todos from "./pages/Todos";
import { useLiveUpdates } from "./api/events";

// CRT beta mode — lazy-loaded so it doesn't bloat the default bundle
const CrtKanban = lazy(() => import("./pages/crt/Kanban"));
const CrtProjectDetail = lazy(() => import("./pages/crt/ProjectDetail"));
const CrtTodos = lazy(() => import("./pages/crt/Todos"));
const BootSequence = lazy(() => import("./components/BootSequence"));

type Phosphor = "green" | "amber" | "white" | "blue";

const PHOSPHOR_OPTIONS: { value: Phosphor; label: string }[] = [
  { value: "green", label: "P1" },
  { value: "amber", label: "P3" },
  { value: "white", label: "DEC" },
  { value: "blue",  label: "BBS" },
];

function useBetaMode(): [boolean, (v: boolean) => void] {
  const [beta, setBeta] = useState(() => localStorage.getItem("petboard-beta") === "1");
  const toggle = (v: boolean) => {
    localStorage.setItem("petboard-beta", v ? "1" : "0");
    setBeta(v);
  };
  return [beta, toggle];
}

function CrtShell() {
  const [booted, setBooted] = useState(false);
  const [phosphor, setPhosphor] = useState<Phosphor>(
    () => (localStorage.getItem("petboard-phosphor") as Phosphor) || "green"
  );
  const [showSwitcher, setShowSwitcher] = useState(false);

  useEffect(() => {
    document.body.setAttribute("data-phosphor", phosphor);
    localStorage.setItem("petboard-phosphor", phosphor);
  }, [phosphor]);

  // Dynamically load audio side-effects in CRT mode
  useEffect(() => { import("./lib/audio"); }, []);

  // Dynamically load CRT CSS
  useEffect(() => {
    import("./crt.css");
    return () => {
      document.body.removeAttribute("data-phosphor");
    };
  }, []);

  return (
    <div className="crt crt-flicker" style={{ position: 'fixed', inset: 0 }}>
      <Suspense fallback={<div style={{ color: 'var(--phos-fg-dim, #888)', padding: 40, fontFamily: 'monospace' }}>Loading...</div>}>
        <Routes>
          <Route path="/" element={<CrtKanban />} />
          <Route path="/p/:slug" element={<CrtProjectDetail />} />
          <Route path="/todos" element={<CrtTodos />} />
        </Routes>
        {!booted && <BootSequence onDone={() => setBooted(true)} />}
      </Suspense>

      {/* Phosphor switcher */}
      <div style={{
        position: 'fixed', bottom: 8, right: 12, zIndex: 90,
        fontFamily: 'var(--font-mono, monospace)', fontSize: 10,
      }}>
        {showSwitcher ? (
          <div style={{
            background: 'var(--phos-bg-deep)', border: '1px solid var(--phos-fg-dim)',
            padding: '6px 8px', display: 'flex', gap: 6,
          }}>
            {PHOSPHOR_OPTIONS.map(opt => (
              <span key={opt.value} data-click="1"
                onClick={() => { setPhosphor(opt.value); setShowSwitcher(false); }}
                style={{
                  cursor: 'pointer', padding: '2px 6px',
                  background: phosphor === opt.value ? 'var(--phos-fg)' : 'transparent',
                  color: phosphor === opt.value ? 'var(--phos-bg-deep)' : 'var(--phos-fg-dim)',
                  textShadow: phosphor === opt.value ? 'none' : undefined,
                  border: '1px solid var(--phos-fg-faint)',
                }}
              >{opt.label}</span>
            ))}
          </div>
        ) : (
          <span data-click="1" style={{ cursor: 'pointer', color: 'var(--phos-fg-faint, #666)', letterSpacing: '0.1em' }}
            onClick={() => setShowSwitcher(true)}>CRT</span>
        )}
      </div>
    </div>
  );
}

function ClassicShell() {
  return (
    <Routes>
      <Route path="/" element={<Kanban />} />
      <Route path="/p/:slug" element={<ProjectDetail />} />
      <Route path="/todos" element={<Todos />} />
    </Routes>
  );
}

export default function App() {
  useLiveUpdates();
  const [beta, setBeta] = useBetaMode();

  return (
    <>
      {beta ? <CrtShell /> : <ClassicShell />}

      {/* Beta toggle — always visible */}
      <button
        type="button"
        onClick={() => setBeta(!beta)}
        title={beta ? "Switch to classic view" : "Try CRT beta"}
        style={{
          position: 'fixed',
          top: 8,
          right: 12,
          zIndex: 200,
          padding: '3px 8px',
          fontSize: 10,
          fontFamily: 'monospace',
          letterSpacing: '0.05em',
          background: beta ? '#222' : 'transparent',
          color: beta ? '#0f0' : '#666',
          border: `1px solid ${beta ? '#0f04' : '#444'}`,
          borderRadius: 3,
          cursor: 'pointer',
          textTransform: 'uppercase',
        }}
      >
        {beta ? 'classic' : 'beta'}
      </button>
    </>
  );
}
