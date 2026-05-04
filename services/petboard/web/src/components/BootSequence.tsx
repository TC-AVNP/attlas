import { useState, useEffect, useRef } from "react";
import * as audio from "../lib/audio";

interface BootLine {
  d: number;
  t: string;
  invert?: boolean;
  modem?: boolean;
}

export default function BootSequence({ onDone }: { onDone: () => void }) {
  const [lines, setLines] = useState<(BootLine & { idx: number })[]>([]);
  const [done, setDone] = useState(false);
  const skipRef = useRef(false);

  useEffect(() => {
    let cancelled = false;

    const script: BootLine[] = [
      { d: 80,  t: 'PETBOARD BIOS v4.21    (c) commonlisp6 industries 1986-2026' },
      { d: 60,  t: 'CPU....... A-CLAUDE-HAIKU-4.5 @ 3.20 THz    [ OK ]' },
      { d: 60,  t: 'MEMORY.... 640K BASE  +  4194304K HIMEM     [ OK ]' },
      { d: 80,  t: 'STORAGE... SQLITE-3 ON /var/lib/petboard.db [ OK ]' },
      { d: 80,  t: 'NETWORK... CADDY @ 127.0.0.1:443            [ OK ]' },
      { d: 100, t: 'AUTH...... GOOGLE FEDERATION (cached)       [ OK ]' },
      { d: 60,  t: '' },
      { d: 60,  t: 'Connecting to MCP transport...' },
      { d: 200, t: 'OAuth 2.1 / PKCE handshake.................. ok', modem: true },
      { d: 80,  t: 'SSE channel /api/stream..................... ok' },
      { d: 80,  t: '' },
      { d: 60,  t: 'PRESS ANY KEY TO BEGIN.', invert: true },
    ];

    let i = 0;
    function tick() {
      if (cancelled) return;
      if (skipRef.current) {
        setLines(script.map((s, j) => ({ ...s, idx: j })));
        setTimeout(() => !cancelled && onDone(), 80);
        return;
      }
      if (i >= script.length) {
        setDone(true);
        setTimeout(() => !cancelled && onDone(), 1100);
        return;
      }
      const s = script[i];
      if (s.modem) audio.modem();
      else if (s.t) audio.click(0.05);
      setLines(prev => [...prev, { ...s, idx: i }]);
      i++;
      setTimeout(tick, s.d);
    }
    tick();
    return () => { cancelled = true; };
  }, [onDone]);

  function skip() {
    skipRef.current = true;
    onDone();
  }

  useEffect(() => {
    const k = () => skip();
    window.addEventListener('keydown', k);
    window.addEventListener('click', k);
    return () => {
      window.removeEventListener('keydown', k);
      window.removeEventListener('click', k);
    };
  }, []);

  return (
    <div style={{
      position: 'absolute', inset: 0,
      padding: '32px 40px',
      fontFamily: 'var(--font-mono)',
      fontSize: 14,
      whiteSpace: 'pre-wrap',
      color: 'var(--phos-fg)',
      zIndex: 80,
      background: 'var(--phos-bg-deep)',
      cursor: 'pointer',
    }}>
      {lines.map((l, idx) => (
        <div key={l.idx} className={l.invert ? 'blink-slow' : ''}>
          <span className={l.invert ? 'crt-reverse' : ''} style={{ padding: l.invert ? '0 6px' : 0 }}>
            {l.t || '\u00a0'}
          </span>
          {idx === lines.length - 1 && !done && !l.invert && <span className="cursor-inline" />}
        </div>
      ))}
    </div>
  );
}
