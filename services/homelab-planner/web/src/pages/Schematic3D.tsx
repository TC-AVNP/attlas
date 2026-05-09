import { Suspense, useRef, useState, useMemo } from "react";
import { Link } from "react-router-dom";
import { Canvas, useFrame } from "@react-three/fiber";
import { OrbitControls, Html, useGLTF } from "@react-three/drei";
import * as THREE from "three";

const BASE = "/homelab-planner";

interface ComponentDef {
  id: string;
  label: string;
  model: string;
  color: string;
  position: [number, number, number];
  specs: string[];
}

const COMPONENTS: ComponentDef[] = [
  {
    id: "worker",
    label: "Pi 5 — Worker",
    model: `${BASE}/models/pi5.glb`,
    color: "#6366f1",
    position: [-8, 0, -4],
    specs: [
      "Raspberry Pi 5 16GB",
      "GeeekPi N05 NVMe HAT",
      "Crucial P310 1TB NVMe",
      "Boots from NVMe",
    ],
  },
  {
    id: "ai",
    label: "Pi 5 — AI Node",
    model: `${BASE}/models/pi5.glb`,
    color: "#a855f7",
    position: [8, 0, -4],
    specs: [
      "Raspberry Pi 5 16GB",
      "AI HAT+ 2 (40 TOPS)",
      "128GB microSD boot",
    ],
  },
  {
    id: "aihat",
    label: "AI HAT+ 2",
    model: `${BASE}/models/aihat.glb`,
    color: "#f59e0b",
    position: [8, 3, -4],
    specs: [
      "Hailo-10H, 40 TOPS",
      "8GB dedicated RAM",
      "Stacked on AI node",
    ],
  },
  {
    id: "router",
    label: "Pi 3B+ — Router",
    model: `${BASE}/models/pi3bp.glb`,
    color: "#f59e0b",
    position: [0, 0, 8],
    specs: [
      "Raspberry Pi 3 Model B+",
      "Waveshare SIM7600G-H 4G HAT",
      "WiFi hotspot + NAT",
    ],
  },
];

function HardwareModel({ def, isActive, onSelect }: { def: ComponentDef; isActive: boolean; onSelect: () => void }) {
  const { scene } = useGLTF(def.model);
  const ref = useRef<THREE.Group>(null!);
  const [hovered, setHovered] = useState(false);

  const cloned = useMemo(() => {
    const s = scene.clone(true);
    s.traverse((node) => {
      const mesh = node as THREE.Mesh;
      if (mesh.isMesh) {
        mesh.material = new THREE.MeshStandardMaterial({
          color: def.color,
          metalness: 0.35,
          roughness: 0.55,
        });
      }
    });
    return s;
  }, [scene, def.color]);

  useFrame((state) => {
    if (ref.current) {
      ref.current.position.y =
        def.position[1] + Math.sin(state.clock.elapsedTime * 0.6 + def.position[0] * 0.5) * 0.15;
    }
  });

  return (
    <group
      ref={ref}
      position={def.position}
      onPointerOver={() => { setHovered(true); document.body.style.cursor = "pointer"; }}
      onPointerOut={() => { setHovered(false); document.body.style.cursor = "auto"; }}
      onClick={onSelect}
    >
      <primitive object={cloned} />

      {(hovered || isActive) && (
        <pointLight position={[0, 3, 0]} intensity={2} color={def.color} distance={12} />
      )}

      <Html position={[0, 4, 0]} center distanceFactor={80}>
        <div style={{
          background: "rgba(0,0,0,0.85)",
          color: "#fff",
          padding: "3px 10px",
          borderRadius: 6,
          fontSize: 12,
          fontWeight: 600,
          whiteSpace: "nowrap",
          border: `1px solid ${def.color}`,
          pointerEvents: "none",
        }}>
          {def.label}
        </div>
      </Html>
    </group>
  );
}

function EthSwitch({ isActive, onSelect }: { isActive: boolean; onSelect: () => void }) {
  const ref = useRef<THREE.Group>(null!);
  const [hovered, setHovered] = useState(false);

  useFrame((state) => {
    if (ref.current) {
      ref.current.position.y = Math.sin(state.clock.elapsedTime * 0.6 + 2) * 0.15;
    }
  });

  return (
    <group
      ref={ref}
      position={[0, 0, 1.5]}
      onPointerOver={() => { setHovered(true); document.body.style.cursor = "pointer"; }}
      onPointerOut={() => { setHovered(false); document.body.style.cursor = "auto"; }}
      onClick={onSelect}
    >
      <mesh>
        <boxGeometry args={[6, 0.6, 3]} />
        <meshStandardMaterial color="#374151" metalness={0.7} roughness={0.25} />
      </mesh>
      {[-2, -1, 0, 1, 2].map((x, i) => (
        <mesh key={i} position={[x, 0, -1.55]}>
          <boxGeometry args={[0.5, 0.4, 0.15]} />
          <meshStandardMaterial
            color={i < 3 ? "#10b981" : "#555"}
            emissive={i < 3 ? "#10b981" : "#000"}
            emissiveIntensity={i < 3 ? 0.5 : 0}
          />
        </mesh>
      ))}
      {(hovered || isActive) && (
        <pointLight position={[0, 2, 0]} intensity={2} color="#10b981" distance={10} />
      )}
      <Html position={[0, 2, 0]} center distanceFactor={80}>
        <div style={{
          background: "rgba(0,0,0,0.85)",
          color: "#fff",
          padding: "3px 10px",
          borderRadius: 6,
          fontSize: 12,
          fontWeight: 600,
          whiteSpace: "nowrap",
          border: "1px solid #10b981",
          pointerEvents: "none",
        }}>
          TP-Link TL-SG105
        </div>
      </Html>
    </group>
  );
}

function Cable({ from, to, color = "#4ade80" }: { from: [number, number, number]; to: [number, number, number]; color?: string }) {
  const curve = useMemo(() => {
    const mid: THREE.Vector3 = new THREE.Vector3(
      (from[0] + to[0]) / 2,
      Math.min(from[1], to[1]) - 1.5,
      (from[2] + to[2]) / 2,
    );
    return new THREE.QuadraticBezierCurve3(
      new THREE.Vector3(...from), mid, new THREE.Vector3(...to),
    );
  }, [from, to]);

  const geometry = useMemo(() => {
    return new THREE.TubeGeometry(curve, 20, 0.08, 6, false);
  }, [curve]);

  return (
    <mesh geometry={geometry}>
      <meshStandardMaterial color={color} metalness={0.3} roughness={0.7} />
    </mesh>
  );
}

function WifiWaves({ position }: { position: [number, number, number] }) {
  const ref = useRef<THREE.Group>(null!);
  useFrame((state) => {
    if (ref.current) {
      ref.current.children.forEach((child, i) => {
        const t = (state.clock.elapsedTime * 0.5 + i * 0.33) % 1;
        child.scale.setScalar(1 + t * 3);
        (child as THREE.Mesh).material && ((child as THREE.Mesh).material as THREE.MeshBasicMaterial).opacity !== undefined &&
          ((child as THREE.Mesh).material as THREE.MeshBasicMaterial).opacity;
        ((child as THREE.Mesh).material as any).opacity = 0.35 * (1 - t);
      });
    }
  });

  return (
    <group ref={ref} position={position}>
      {[0, 1, 2].map((i) => (
        <mesh key={i} rotation={[Math.PI / 2, 0, 0]}>
          <ringGeometry args={[0.5, 0.6, 32]} />
          <meshBasicMaterial color="#f59e0b" transparent opacity={0.35} side={THREE.DoubleSide} />
        </mesh>
      ))}
    </group>
  );
}

function Antenna({ position }: { position: [number, number, number] }) {
  return (
    <group position={position}>
      <mesh position={[0, 1.5, 0]}>
        <cylinderGeometry args={[0.06, 0.06, 3, 8]} />
        <meshStandardMaterial color="#aaa" metalness={0.8} roughness={0.2} />
      </mesh>
      <mesh position={[0, 3.2, 0]}>
        <sphereGeometry args={[0.15, 8, 8]} />
        <meshBasicMaterial color="#ef4444" />
      </mesh>
      <Html position={[0, 4, 0]} center distanceFactor={80}>
        <div style={{ background: "rgba(239,68,68,0.9)", color: "#fff", padding: "2px 8px", borderRadius: 4, fontSize: 11, fontWeight: 700, pointerEvents: "none" }}>
          4G LTE
        </div>
      </Html>
    </group>
  );
}

function Scene() {
  const [selected, setSelected] = useState<string | null>(null);

  const switchSpecs = ["TP-Link TL-SG105", "5-port Gigabit", "Metal case, QoS"];
  const activeComp = COMPONENTS.find((c) => c.id === selected);
  const activeSpecs = selected === "switch" ? switchSpecs : activeComp?.specs || [];
  const activeLabel = selected === "switch" ? "TP-Link TL-SG105" : activeComp?.label || "";
  const activeColor = selected === "switch" ? "#10b981" : activeComp?.color || "#fff";

  return (
    <>
      <ambientLight intensity={0.5} />
      <directionalLight position={[15, 15, 10]} intensity={1} />
      <directionalLight position={[-10, 8, -10]} intensity={0.4} />
      <hemisphereLight args={["#b1e1ff", "#333", 0.5]} />

      {/* Ground */}
      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -2, 0]} receiveShadow>
        <planeGeometry args={[50, 40]} />
        <meshStandardMaterial color="#0f172a" />
      </mesh>

      {/* Hardware models */}
      {COMPONENTS.map((c) => (
        <HardwareModel
          key={c.id}
          def={c}
          isActive={selected === c.id}
          onSelect={() => setSelected(selected === c.id ? null : c.id)}
        />
      ))}

      <EthSwitch
        isActive={selected === "switch"}
        onSelect={() => setSelected(selected === "switch" ? null : "switch")}
      />

      {/* Ethernet cables */}
      <Cable from={[-8, -1, -4]} to={[-2.5, -1, 1.5]} />
      <Cable from={[8, -1, -4]} to={[2.5, -1, 1.5]} />
      <Cable from={[0, -1, 8]} to={[0, -1, 3]} />

      {/* WiFi + antenna on router */}
      <WifiWaves position={[0, 3, 8]} />
      <Antenna position={[2, 1, 8]} />

      <OrbitControls
        enablePan
        enableZoom
        minDistance={8}
        maxDistance={40}
        target={[0, 0, 2]}
      />

      {/* Spec panel */}
      {selected && (
        <Html position={[-15, 8, 0]}>
          <div style={{
            background: "rgba(0,0,0,0.92)",
            border: `1px solid ${activeColor}`,
            borderRadius: 10,
            padding: "14px 18px",
            minWidth: 200,
            pointerEvents: "none",
          }}>
            <div style={{ fontSize: 15, fontWeight: 700, color: "#fff", marginBottom: 8 }}>
              {activeLabel}
            </div>
            {activeSpecs.map((s, i) => (
              <div key={i} style={{ fontSize: 12, color: "#9ca3af", lineHeight: 1.7 }}>{s}</div>
            ))}
          </div>
        </Html>
      )}
    </>
  );
}

export default function Schematic3D() {
  return (
    <div className="w-full h-screen bg-gray-950 relative">
      <div className="absolute top-4 left-4 z-10 flex items-center gap-4">
        <Link to="/" className="text-sm text-gray-400 hover:text-gray-200 bg-gray-900/80 px-3 py-1.5 rounded">
          &larr; Back
        </Link>
        <h1 className="text-lg font-bold text-white">Homelab 3D</h1>
      </div>
      <div className="absolute bottom-4 left-4 z-10 text-xs text-gray-500 bg-gray-900/80 px-3 py-2 rounded">
        Drag to rotate · Scroll to zoom · Click components
      </div>
      <div className="absolute top-4 right-4 z-10 bg-gray-900/80 px-4 py-2 rounded">
        <span className="text-xs text-gray-400">Total </span>
        <span className="text-lg font-bold text-green-400">&euro;1,156</span>
      </div>

      <Canvas camera={{ position: [20, 14, 20], fov: 45 }}>
        <Suspense fallback={<Html center><span style={{ color: "#fff", fontSize: 16 }}>Loading models...</span></Html>}>
          <Scene />
        </Suspense>
      </Canvas>
    </div>
  );
}
