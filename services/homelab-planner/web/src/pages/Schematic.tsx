import { Link } from "react-router-dom";

export default function Schematic() {
  return (
    <div className="max-w-5xl mx-auto p-6">
      <Link
        to="/"
        className="text-sm text-gray-400 hover:text-gray-200 mb-4 inline-block"
      >
        &larr; Back
      </Link>

      <div className="flex items-center justify-between mb-6">
        <h1 className="text-2xl font-bold">Network Schematic</h1>
        <span className="px-3 py-1.5 bg-indigo-600/20 border border-indigo-500 rounded-lg text-sm font-semibold text-indigo-300">
          Total: &euro;1,124
        </span>
      </div>

      <div className="bg-gray-900 rounded-xl p-4 overflow-x-auto">
        <svg
          viewBox="0 0 900 720"
          className="w-full h-auto"
          xmlns="http://www.w3.org/2000/svg"
          style={{ minWidth: 600 }}
        >
          <defs>
            {/* Arrowhead marker */}
            <marker
              id="arrow"
              viewBox="0 0 10 7"
              refX="10"
              refY="3.5"
              markerWidth="8"
              markerHeight="6"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 10 3.5 L 0 7 z" fill="#6b7280" />
            </marker>

            {/* Glow filters */}
            <filter id="glow-amber">
              <feDropShadow dx="0" dy="0" stdDeviation="4" floodColor="#f59e0b" floodOpacity="0.3" />
            </filter>
            <filter id="glow-emerald">
              <feDropShadow dx="0" dy="0" stdDeviation="4" floodColor="#10b981" floodOpacity="0.3" />
            </filter>
            <filter id="glow-indigo">
              <feDropShadow dx="0" dy="0" stdDeviation="4" floodColor="#6366f1" floodOpacity="0.3" />
            </filter>
          </defs>

          {/* ==================== */}
          {/* 4G/LTE Internet Cloud */}
          {/* ==================== */}
          <g transform="translate(450, 45)">
            <ellipse
              cx="0"
              cy="0"
              rx="90"
              ry="32"
              fill="#1e1b4b"
              stroke="#818cf8"
              strokeWidth="1.5"
              strokeDasharray="6 3"
            />
            <text
              x="0"
              y="-6"
              textAnchor="middle"
              fill="#a5b4fc"
              fontSize="12"
              fontWeight="600"
            >
              4G / LTE
            </text>
            <text
              x="0"
              y="10"
              textAnchor="middle"
              fill="#a5b4fc"
              fontSize="11"
            >
              Internet
            </text>
          </g>

          {/* 4G connection line: cloud -> router */}
          <line
            x1="450"
            y1="77"
            x2="450"
            y2="130"
            stroke="#818cf8"
            strokeWidth="2"
            strokeDasharray="6 3"
            markerEnd="url(#arrow)"
          />
          <text
            x="470"
            y="108"
            fill="#818cf8"
            fontSize="10"
            fontStyle="italic"
          >
            4G
          </text>

          {/* ==================== */}
          {/* Antenna icon on top of router */}
          {/* ==================== */}
          <g transform="translate(450, 125)">
            {/* Antenna mast */}
            <line x1="0" y1="0" x2="0" y2="-15" stroke="#f59e0b" strokeWidth="2" />
            <circle cx="0" cy="-18" r="3" fill="#f59e0b" />
            {/* Signal arcs */}
            <path
              d="M -8,-25 A 10,10 0 0,1 8,-25"
              fill="none"
              stroke="#f59e0b"
              strokeWidth="1.5"
              opacity="0.6"
            />
            <path
              d="M -14,-30 A 16,16 0 0,1 14,-30"
              fill="none"
              stroke="#f59e0b"
              strokeWidth="1.5"
              opacity="0.4"
            />
          </g>

          {/* ==================== */}
          {/* Pi 3B+ Router Node */}
          {/* ==================== */}
          <g filter="url(#glow-amber)">
            <rect
              x="320"
              y="130"
              width="260"
              height="110"
              rx="12"
              fill="#111827"
              stroke="#f59e0b"
              strokeWidth="2"
            />
          </g>
          {/* Amber accent bar */}
          <rect x="320" y="130" width="260" height="4" rx="2" fill="#f59e0b" opacity="0.6" />
          {/* Node label */}
          <text x="340" y="157" fill="#fbbf24" fontSize="14" fontWeight="700">
            Pi 3B+ Router
          </text>
          <text x="340" y="175" fill="#9ca3af" fontSize="11">
            Waveshare SIM7600G-H 4G HAT
          </text>
          <text x="340" y="191" fill="#9ca3af" fontSize="11">
            SIM card + external antenna
          </text>
          <text x="340" y="207" fill="#9ca3af" fontSize="11">
            WiFi hotspot + NAT gateway
          </text>
          <text x="340" y="223" fill="#6b7280" fontSize="10">
            Role: router / gateway
          </text>

          {/* WiFi icon next to router */}
          <g transform="translate(555, 165)">
            <path
              d="M -6,6 A 8,8 0 0,1 6,6"
              fill="none"
              stroke="#fbbf24"
              strokeWidth="1.5"
              opacity="0.5"
            />
            <path
              d="M -11,2 A 14,14 0 0,1 11,2"
              fill="none"
              stroke="#fbbf24"
              strokeWidth="1.5"
              opacity="0.35"
            />
            <path
              d="M -16,-2 A 20,20 0 0,1 16,-2"
              fill="none"
              stroke="#fbbf24"
              strokeWidth="1.5"
              opacity="0.2"
            />
            <circle cx="0" cy="8" r="2.5" fill="#fbbf24" />
          </g>

          {/* ==================== */}
          {/* Ethernet: Router -> Switch */}
          {/* ==================== */}
          <line
            x1="450"
            y1="240"
            x2="450"
            y2="330"
            stroke="#6b7280"
            strokeWidth="2"
            markerEnd="url(#arrow)"
          />
          <rect x="420" y="275" width="60" height="18" rx="4" fill="#111827" />
          <text x="450" y="288" textAnchor="middle" fill="#9ca3af" fontSize="10">
            Cat6
          </text>

          {/* ==================== */}
          {/* TP-Link Switch Node */}
          {/* ==================== */}
          <g filter="url(#glow-emerald)">
            <rect
              x="320"
              y="330"
              width="260"
              height="90"
              rx="12"
              fill="#111827"
              stroke="#10b981"
              strokeWidth="2"
            />
          </g>
          <rect x="320" y="330" width="260" height="4" rx="2" fill="#10b981" opacity="0.6" />
          <text x="340" y="358" fill="#34d399" fontSize="14" fontWeight="700">
            TP-Link LS1005G
          </text>
          <text x="340" y="376" fill="#9ca3af" fontSize="11">
            5-port gigabit ethernet switch
          </text>
          <text x="340" y="392" fill="#9ca3af" fontSize="11">
            Unmanaged, fanless
          </text>
          <text x="340" y="408" fill="#6b7280" fontSize="10">
            Role: LAN backbone
          </text>

          {/* Port indicators */}
          <g transform="translate(530, 395)">
            {[0, 1, 2, 3, 4].map((i) => (
              <rect
                key={i}
                x={i * 12}
                y="0"
                width="8"
                height="12"
                rx="1"
                fill={i < 3 ? "#10b981" : "#374151"}
                opacity={i < 3 ? 0.7 : 0.4}
              />
            ))}
          </g>

          {/* ==================== */}
          {/* Ethernet: Switch -> Worker Pi 5 */}
          {/* ==================== */}
          <line
            x1="380"
            y1="420"
            x2="230"
            y2="520"
            stroke="#6b7280"
            strokeWidth="2"
            markerEnd="url(#arrow)"
          />
          <rect x="268" y="458" width="60" height="18" rx="4" fill="#111827" />
          <text x="298" y="471" textAnchor="middle" fill="#9ca3af" fontSize="10">
            Cat6
          </text>

          {/* ==================== */}
          {/* Ethernet: Switch -> AI Node Pi 5 */}
          {/* ==================== */}
          <line
            x1="520"
            y1="420"
            x2="670"
            y2="520"
            stroke="#6b7280"
            strokeWidth="2"
            markerEnd="url(#arrow)"
          />
          <rect x="570" y="458" width="60" height="18" rx="4" fill="#111827" />
          <text x="600" y="471" textAnchor="middle" fill="#9ca3af" fontSize="10">
            Cat6
          </text>

          {/* ==================== */}
          {/* WiFi: Router -> Worker Pi 5 (dashed) */}
          {/* ==================== */}
          <path
            d="M 345,240 C 310,350 200,420 180,520"
            fill="none"
            stroke="#fbbf24"
            strokeWidth="1.5"
            strokeDasharray="5 4"
            opacity="0.5"
          />
          <text x="235" y="400" fill="#fbbf24" fontSize="9" opacity="0.7" transform="rotate(-50, 235, 400)">
            WiFi
          </text>

          {/* ==================== */}
          {/* WiFi: Router -> AI Node Pi 5 (dashed) */}
          {/* ==================== */}
          <path
            d="M 555,240 C 590,350 700,420 720,520"
            fill="none"
            stroke="#fbbf24"
            strokeWidth="1.5"
            strokeDasharray="5 4"
            opacity="0.5"
          />
          <text x="650" y="400" fill="#fbbf24" fontSize="9" opacity="0.7" transform="rotate(50, 650, 400)">
            WiFi
          </text>

          {/* ==================== */}
          {/* Pi 5 Worker Node */}
          {/* ==================== */}
          <g filter="url(#glow-indigo)">
            <rect
              x="70"
              y="520"
              width="280"
              height="130"
              rx="12"
              fill="#111827"
              stroke="#6366f1"
              strokeWidth="2"
            />
          </g>
          <rect x="70" y="520" width="280" height="4" rx="2" fill="#6366f1" opacity="0.6" />
          <text x="90" y="548" fill="#a5b4fc" fontSize="14" fontWeight="700">
            Pi 5 Worker Node
          </text>
          <text x="90" y="566" fill="#9ca3af" fontSize="11">
            GeeekPi N05 NVMe HAT
          </text>
          <text x="90" y="582" fill="#9ca3af" fontSize="11">
            Crucial P310 1TB NVMe SSD
          </text>
          <text x="90" y="598" fill="#9ca3af" fontSize="11">
            Boot from NVMe, 8GB RAM
          </text>
          <text x="90" y="614" fill="#9ca3af" fontSize="11">
            Active cooler
          </text>
          <text x="90" y="636" fill="#6b7280" fontSize="10">
            Role: K3s worker, storage
          </text>

          {/* NVMe icon */}
          <g transform="translate(310, 570)">
            <rect x="0" y="0" width="28" height="10" rx="2" fill="#4f46e5" opacity="0.5" />
            <text x="14" y="8" textAnchor="middle" fill="#c7d2fe" fontSize="7" fontWeight="600">
              NVMe
            </text>
          </g>

          {/* ==================== */}
          {/* Pi 5 AI Node */}
          {/* ==================== */}
          <g filter="url(#glow-indigo)">
            <rect
              x="550"
              y="520"
              width="280"
              height="130"
              rx="12"
              fill="#111827"
              stroke="#6366f1"
              strokeWidth="2"
            />
          </g>
          <rect x="550" y="520" width="280" height="4" rx="2" fill="#6366f1" opacity="0.6" />
          <text x="570" y="548" fill="#a5b4fc" fontSize="14" fontWeight="700">
            Pi 5 AI Node
          </text>
          <text x="570" y="566" fill="#9ca3af" fontSize="11">
            AI HAT+ 26 TOPS (Hailo-8L)
          </text>
          <text x="570" y="582" fill="#9ca3af" fontSize="11">
            128GB microSD boot
          </text>
          <text x="570" y="598" fill="#9ca3af" fontSize="11">
            8GB RAM, active cooler
          </text>
          <text x="570" y="614" fill="#9ca3af" fontSize="11">
            No USB NVMe enclosure needed
          </text>
          <text x="570" y="636" fill="#6b7280" fontSize="10">
            Role: AI inference, edge ML
          </text>

          {/* AI chip icon */}
          <g transform="translate(790, 558)">
            <rect x="0" y="0" width="24" height="24" rx="3" fill="#4f46e5" opacity="0.5" />
            {/* Chip pins */}
            {[4, 10, 16].map((p) => (
              <g key={p}>
                <line x1={p} y1="-3" x2={p} y2="0" stroke="#818cf8" strokeWidth="1.5" />
                <line x1={p} y1="24" x2={p} y2="27" stroke="#818cf8" strokeWidth="1.5" />
              </g>
            ))}
            {[4, 10, 16].map((p) => (
              <g key={p}>
                <line x1="-3" y1={p} x2="0" y2={p} stroke="#818cf8" strokeWidth="1.5" />
                <line x1="24" y1={p} x2="27" y2={p} stroke="#818cf8" strokeWidth="1.5" />
              </g>
            ))}
            <text x="12" y="15" textAnchor="middle" fill="#c7d2fe" fontSize="7" fontWeight="700">
              AI
            </text>
          </g>

          {/* ==================== */}
          {/* Legend */}
          {/* ==================== */}
          <g transform="translate(30, 680)">
            <line x1="0" y1="0" x2="24" y2="0" stroke="#6b7280" strokeWidth="2" />
            <text x="30" y="4" fill="#6b7280" fontSize="10">
              Ethernet (Cat6)
            </text>

            <line
              x1="140"
              y1="0"
              x2="164"
              y2="0"
              stroke="#fbbf24"
              strokeWidth="1.5"
              strokeDasharray="5 4"
            />
            <text x="170" y="4" fill="#6b7280" fontSize="10">
              WiFi
            </text>

            <line
              x1="230"
              y1="0"
              x2="254"
              y2="0"
              stroke="#818cf8"
              strokeWidth="2"
              strokeDasharray="6 3"
            />
            <text x="260" y="4" fill="#6b7280" fontSize="10">
              4G/LTE
            </text>

            {/* Color key */}
            <rect x="370" y="-5" width="10" height="10" rx="2" fill="#f59e0b" opacity="0.7" />
            <text x="386" y="4" fill="#6b7280" fontSize="10">
              Router
            </text>

            <rect x="440" y="-5" width="10" height="10" rx="2" fill="#10b981" opacity="0.7" />
            <text x="456" y="4" fill="#6b7280" fontSize="10">
              Networking
            </text>

            <rect x="530" y="-5" width="10" height="10" rx="2" fill="#6366f1" opacity="0.7" />
            <text x="546" y="4" fill="#6b7280" fontSize="10">
              Compute
            </text>
          </g>
        </svg>
      </div>
    </div>
  );
}
