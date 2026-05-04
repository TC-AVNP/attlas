import { useEffect, useState } from "react";
import * as audio from "../lib/audio";

export default function KernelPanic({ projectName, onDismiss }: { projectName: string; onDismiss: () => void }) {
  const [, setTick] = useState(0);

  useEffect(() => {
    audio.beep(220, 0.4, 0.08);
    setTimeout(() => audio.beep(180, 0.4, 0.08), 250);
    const id = setInterval(() => setTick(t => t + 1), 1000);
    const k = () => onDismiss();
    window.addEventListener('keydown', k);
    window.addEventListener('click', k);
    return () => { clearInterval(id); window.removeEventListener('keydown', k); window.removeEventListener('click', k); };
  }, [onDismiss]);

  const W = 78;
  const top    = '\u2554' + '\u2550'.repeat(W - 2) + '\u2557';
  const bot    = '\u255a' + '\u2550'.repeat(W - 2) + '\u255d';
  const blank  = '\u2551' + ' '.repeat(W - 2) + '\u2551';
  function row(s: string) {
    const inner = ' ' + s + ' '.repeat(Math.max(0, W - 3 - s.length));
    return '\u2551' + inner.slice(0, W - 2) + '\u2551';
  }

  return (
    <div className="modal-backdrop" onClick={onDismiss} style={{ cursor: 'pointer' }}>
      <pre className="ascii-pre blink-slow" style={{
        color: 'var(--danger)',
        fontFamily: 'var(--font-mono)',
        fontSize: 'clamp(9px, 1.4vw, 13px)',
        textShadow: '0 0 4px oklch(0.65 0.27 25 / 0.7), 0 0 12px oklch(0.65 0.27 25 / 0.5)',
        margin: 0,
        lineHeight: 1.2,
        maxWidth: '92%',
        overflow: 'hidden',
      }}>
{top + '\n'}
{blank + '\n'}
{row('  ! ! !   K E R N E L   P A N I C   ! ! !') + '\n'}
{blank + '\n'}
{row('  Software Failure.  Press any key to continue.') + '\n'}
{blank + '\n'}
{row('  Guru Meditation #00000004.04A104EC') + '\n'}
{blank + '\n'}
{row('  ENOTIMPL: function not implemented') + '\n'}
{row('     at petboard::work_on_it (' + (projectName || 'unknown') + ')') + '\n'}
{row('     at user::expectation') + '\n'}
{row('     at universe::heat_death  (deferred)') + '\n'}
{blank + '\n'}
{row('  This feature has not been built yet.') + '\n'}
{row('  See feature #84 in the petboard backlog.') + '\n'}
{blank + '\n'}
{row('  Workaround:  copy markdown handoff into a fresh shell.') + '\n'}
{blank + '\n'}
{bot}
      </pre>
    </div>
  );
}
