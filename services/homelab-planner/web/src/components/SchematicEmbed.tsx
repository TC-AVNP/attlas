export default function SchematicEmbed() {
  return (
    <div className="w-full bg-[#111] rounded-md border border-[#222] mb-8 p-6 overflow-x-auto">
      <svg
        viewBox="0 0 700 400"
        className="w-full h-auto"
        xmlns="http://www.w3.org/2000/svg"
        style={{ minWidth: 480 }}
      >
        {/* Internet cloud */}
        <ellipse cx="350" cy="40" rx="80" ry="25" fill="none" stroke="#6366f1" strokeWidth="1" strokeDasharray="4 3" opacity="0.6" />
        <text x="350" y="45" textAnchor="middle" fill="#6366f1" fontSize="11" fontFamily="'JetBrains Mono', monospace" opacity="0.8">4G / LTE</text>

        {/* Router */}
        <rect x="280" y="100" width="140" height="65" rx="6" fill="#151515" stroke="#f59e0b" strokeWidth="1" />
        <text x="350" y="122" textAnchor="middle" fill="#f59e0b" fontSize="11" fontFamily="'JetBrains Mono', monospace">Pi 3B+ Router</text>
        <text x="350" y="138" textAnchor="middle" fill="#666" fontSize="9" fontFamily="'JetBrains Mono', monospace">SIM7600G-H 4G HAT</text>
        <text x="350" y="153" textAnchor="middle" fill="#555" fontSize="9" fontFamily="'JetBrains Mono', monospace">NAT + DHCP + WiFi</text>

        {/* Connection: Internet -> Router */}
        <line x1="350" y1="65" x2="350" y2="100" stroke="#6366f1" strokeWidth="1" strokeDasharray="4 3" opacity="0.5" />

        {/* Switch */}
        <rect x="285" y="215" width="130" height="45" rx="6" fill="#151515" stroke="#10b981" strokeWidth="1" />
        <text x="350" y="236" textAnchor="middle" fill="#10b981" fontSize="11" fontFamily="'JetBrains Mono', monospace">TP-Link LS1005G</text>
        <text x="350" y="251" textAnchor="middle" fill="#555" fontSize="9" fontFamily="'JetBrains Mono', monospace">5-port Gigabit</text>

        {/* Port indicators */}
        {[310, 327, 344, 361, 378].map((x, i) => (
          <circle key={i} cx={x} cy="256" r="2.5" fill={i < 3 ? "#10b981" : "#2a2a2a"} opacity={i < 3 ? 0.8 : 0.4} />
        ))}

        {/* Connection: Router -> Switch */}
        <line x1="350" y1="165" x2="350" y2="215" stroke="#444" strokeWidth="1.5" />
        <text x="362" y="193" fill="#444" fontSize="8" fontFamily="'JetBrains Mono', monospace">eth</text>

        {/* Worker Node */}
        <rect x="125" y="315" width="155" height="55" rx="6" fill="#151515" stroke="#6366f1" strokeWidth="1" />
        <text x="202" y="337" textAnchor="middle" fill="#6366f1" fontSize="11" fontFamily="'JetBrains Mono', monospace">Pi 5 — Worker</text>
        <text x="202" y="355" textAnchor="middle" fill="#555" fontSize="9" fontFamily="'JetBrains Mono', monospace">16GB  ·  1TB NVMe  ·  .10</text>

        {/* AI Node */}
        <rect x="420" y="315" width="155" height="55" rx="6" fill="#151515" stroke="#a855f7" strokeWidth="1" />
        <text x="497" y="337" textAnchor="middle" fill="#a855f7" fontSize="11" fontFamily="'JetBrains Mono', monospace">Pi 5 — AI Node</text>
        <text x="497" y="355" textAnchor="middle" fill="#555" fontSize="9" fontFamily="'JetBrains Mono', monospace">16GB  ·  AI HAT+  ·  .11</text>

        {/* Connection: Switch -> Worker */}
        <line x1="315" y1="260" x2="202" y2="315" stroke="#444" strokeWidth="1.5" />
        <text x="245" y="292" fill="#444" fontSize="8" fontFamily="'JetBrains Mono', monospace">cat6</text>

        {/* Connection: Switch -> AI */}
        <line x1="385" y1="260" x2="497" y2="315" stroke="#444" strokeWidth="1.5" />
        <text x="437" y="292" fill="#444" fontSize="8" fontFamily="'JetBrains Mono', monospace">cat6</text>
      </svg>
    </div>
  );
}
