// Web Audio keystroke clicks, beeps, and modem warble.
// Activates on first user gesture (autoplay policy).

let ctx: AudioContext | null = null;
let masterGain: GainNode | null = null;
let enabled = true;

function init(): AudioContext | null {
  if (ctx) return ctx;
  try {
    const AC = window.AudioContext || (window as any).webkitAudioContext;
    ctx = new AC();
    masterGain = ctx.createGain();
    masterGain.gain.value = 0.10;
    masterGain.connect(ctx.destination);
  } catch {
    ctx = null;
  }
  return ctx;
}

export function click(volume = 0.10) {
  if (!enabled) return;
  const c = init();
  if (!c || !masterGain) return;
  if (c.state === 'suspended') c.resume();

  const dur = 0.025 + Math.random() * 0.012;
  const buffer = c.createBuffer(1, Math.floor(c.sampleRate * dur), c.sampleRate);
  const data = buffer.getChannelData(0);
  for (let i = 0; i < data.length; i++) {
    const t = i / data.length;
    data[i] = (Math.random() * 2 - 1) * Math.exp(-t * 12);
  }
  const src = c.createBufferSource();
  src.buffer = buffer;
  const gain = c.createGain();
  gain.gain.value = volume;
  const filter = c.createBiquadFilter();
  filter.type = 'bandpass';
  filter.frequency.value = 2200 + Math.random() * 800;
  filter.Q.value = 0.7;
  src.connect(filter);
  filter.connect(gain);
  gain.connect(masterGain);
  src.start();
}

export function beep(freq = 880, dur = 0.08, vol = 0.06) {
  if (!enabled) return;
  const c = init();
  if (!c || !masterGain) return;
  if (c.state === 'suspended') c.resume();
  const osc = c.createOscillator();
  const gain = c.createGain();
  osc.type = 'square';
  osc.frequency.value = freq;
  gain.gain.value = 0;
  gain.gain.linearRampToValueAtTime(vol, c.currentTime + 0.005);
  gain.gain.linearRampToValueAtTime(0, c.currentTime + dur);
  osc.connect(gain);
  gain.connect(masterGain);
  osc.start();
  osc.stop(c.currentTime + dur);
}

export function modem() {
  if (!enabled) return;
  const c = init();
  if (!c || !masterGain) return;
  if (c.state === 'suspended') c.resume();
  const tones = [1200, 2100, 1700, 2400, 1100, 2300, 1800];
  let t = c.currentTime;
  for (const f of tones) {
    const osc = c.createOscillator();
    const g = c.createGain();
    osc.type = 'sine';
    osc.frequency.value = f;
    g.gain.value = 0;
    g.gain.linearRampToValueAtTime(0.04, t + 0.02);
    g.gain.linearRampToValueAtTime(0, t + 0.18);
    osc.connect(g); g.connect(masterGain);
    osc.start(t); osc.stop(t + 0.2);
    t += 0.16;
  }
}

export function setEnabled(v: boolean) { enabled = v; }
export function isEnabled() { return enabled; }

// Wire hover/click sounds to interactive elements
let lastClick = 0;
function handler() {
  const now = performance.now();
  if (now - lastClick < 28) return;
  lastClick = now;
  click();
}

document.addEventListener('mouseover', (e) => {
  const t = e.target;
  if (!(t instanceof Element)) return;
  if (t.closest('.proj-card, .btn, [data-click], a')) handler();
}, true);

document.addEventListener('click', (e) => {
  const t = e.target;
  if (!(t instanceof Element)) return;
  if (t.closest('button, .btn, [data-click], a, .proj-card')) {
    click(0.16); beep(1600, 0.04, 0.04);
  }
}, true);
