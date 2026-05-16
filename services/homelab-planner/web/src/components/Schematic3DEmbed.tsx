import { Suspense, useRef, useState, useMemo } from "react";
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

const MODEL_SCALE: [number, number, number] = [0.5, 0.5, 0.5];

const COMPONENTS: ComponentDef[] = [
  {
    id: "worker",
    label: "Pi 5 — Worker",
    model: `${BASE}/models/pi5.glb`,
    color: "#6366f1",
    position: [-4, 0, -2],
    specs: ["Raspberry Pi 5 16GB", "GeeekPi N05 NVMe HAT", "Crucial P310 1TB NVMe", "Boots from NVMe"],
  },
  {
    id: "ai",
    label: "Pi 5 — AI Node",
    model: `${BASE}/models/pi5.glb`,
    color: "#a855f7",
    position: [4, 0, -2],
    specs: ["Raspberry Pi 5 16GB", "AI HAT+ 2 (40 TOPS)", "128GB microSD boot"],
  },
  {
    id: "aihat",
    label: "AI HAT+ 2",
    model: `${BASE}/models/aihat.glb`,
    color: "#f59e0b",
    position: [4, 1.8, -2],
    specs: ["Hailo-10H, 40 TOPS", "8GB dedicated RAM", "Stacked on AI node"],
  },
  {
    id: "router",
    label: "Pi 3B+ — Router",
    model: `${BASE}/models/pi3bp.glb`,
    color: "#f59e0b",
    position: [0, 0, 4.5],
    specs: ["Raspberry Pi 3 Model B+", "Waveshare SIM7600G-H 4G HAT", "WiFi hotspot + NAT"],
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
      if (mesh.isMesh && mesh.material) {
        // Keep the original material but clone it so we can add a subtle emissive tint
        const orig = mesh.material as THREE.MeshStandardMaterial;
        const mat = orig.clone();
        mat.emissive = new THREE.Color(def.color);
        mat.emissiveIntensity = 0.15;
        mesh.material = mat;
      }
    });
    return s;
  }, [scene, def.color]);

  useFrame((state) => {
    if (ref.current) {
      ref.current.position.y =
        def.position[1] + Math.sin(state.clock.elapsedTime * 0.6 + def.position[0] * 0.5) * 0.08;
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
      <primitive object={cloned} scale={MODEL_SCALE} />
      {/* Subtle colored glow always visible for identification */}
      <pointLight position={[0, -0.5, 0]} intensity={0.6} color={def.color} distance={5} />
      {(hovered || isActive) && (
        <pointLight position={[0, 2, 0]} intensity={3} color={def.color} distance={8} />
      )}
      {(hovered || isActive) && (
        <Html position={[0, 2, 0]} center distanceFactor={40}>
          <div style={{
            background: "rgba(0,0,0,0.85)", color: "#fff", padding: "2px 8px",
            borderRadius: 4, fontSize: 10, fontWeight: 600, whiteSpace: "nowrap",
            border: `1px solid ${def.color}`, pointerEvents: "none",
          }}>
            {def.label}
          </div>
        </Html>
      )}
    </group>
  );
}

function EthSwitch({ isActive, onSelect }: { isActive: boolean; onSelect: () => void }) {
  const ref = useRef<THREE.Group>(null!);
  const [hovered, setHovered] = useState(false);

  useFrame((state) => {
    if (ref.current) {
      ref.current.position.y = Math.sin(state.clock.elapsedTime * 0.6 + 2) * 0.08;
    }
  });

  return (
    <group
      ref={ref}
      position={[0, 0, 1]}
      onPointerOver={() => { setHovered(true); document.body.style.cursor = "pointer"; }}
      onPointerOut={() => { setHovered(false); document.body.style.cursor = "auto"; }}
      onClick={onSelect}
    >
      <mesh>
        <boxGeometry args={[3, 0.3, 1.5]} />
        <meshStandardMaterial color="#374151" metalness={0.7} roughness={0.25} />
      </mesh>
      {[-1, -0.5, 0, 0.5, 1].map((x, i) => (
        <mesh key={i} position={[x, 0, -0.8]}>
          <boxGeometry args={[0.25, 0.2, 0.08]} />
          <meshStandardMaterial
            color={i < 3 ? "#10b981" : "#555"}
            emissive={i < 3 ? "#10b981" : "#000"}
            emissiveIntensity={i < 3 ? 0.5 : 0}
          />
        </mesh>
      ))}
      {(hovered || isActive) && (
        <pointLight position={[0, 1.5, 0]} intensity={2} color="#10b981" distance={6} />
      )}
      {(hovered || isActive) && (
        <Html position={[0, 1.2, 0]} center distanceFactor={40}>
          <div style={{
            background: "rgba(0,0,0,0.85)", color: "#fff", padding: "2px 8px",
            borderRadius: 4, fontSize: 10, fontWeight: 600, whiteSpace: "nowrap",
            border: "1px solid #10b981", pointerEvents: "none",
          }}>
            TP-Link TL-SG105
          </div>
        </Html>
      )}
    </group>
  );
}

function Cable({ from, to, color = "#4ade80" }: { from: [number, number, number]; to: [number, number, number]; color?: string }) {
  const curve = useMemo(() => {
    const mid = new THREE.Vector3((from[0] + to[0]) / 2, Math.min(from[1], to[1]) - 0.8, (from[2] + to[2]) / 2);
    return new THREE.QuadraticBezierCurve3(new THREE.Vector3(...from), mid, new THREE.Vector3(...to));
  }, [from, to]);

  const geometry = useMemo(() => new THREE.TubeGeometry(curve, 20, 0.08, 6, false), [curve]);

  return (
    <mesh geometry={geometry}>
      <meshStandardMaterial color={color} metalness={0.3} roughness={0.7} />
    </mesh>
  );
}

function Scene() {
  const [selected, setSelected] = useState<string | null>(null);

  return (
    <>
      <ambientLight intensity={0.8} />
      <directionalLight position={[15, 15, 10]} intensity={1.5} />
      <directionalLight position={[-10, 8, -10]} intensity={0.7} />
      <hemisphereLight args={["#b1e1ff", "#555", 0.6]} />

      <mesh rotation={[-Math.PI / 2, 0, 0]} position={[0, -1.2, 0]}>
        <planeGeometry args={[30, 25]} />
        <meshStandardMaterial color="#0f172a" />
      </mesh>

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

      <Cable from={[-4, -0.5, -2]} to={[-1.2, -0.5, 1]} />
      <Cable from={[4, -0.5, -2]} to={[1.2, -0.5, 1]} />
      <Cable from={[0, -0.5, 4.5]} to={[0, -0.5, 1.8]} />

      <OrbitControls enablePan enableZoom minDistance={6} maxDistance={30} target={[0, 0, 1]} />

      {selected && (() => {
        const switchSpecs = ["TP-Link TL-SG105", "5-port Gigabit", "Fanless, unmanaged"];
        const comp = COMPONENTS.find((c) => c.id === selected);
        const specs = selected === "switch" ? switchSpecs : comp?.specs || [];
        const label = selected === "switch" ? "TP-Link TL-SG105" : comp?.label || "";
        const color = selected === "switch" ? "#10b981" : comp?.color || "#fff";
        return (
          <Html position={[-8, 5, 0]}>
            <div style={{
              background: "rgba(0,0,0,0.92)", border: `1px solid ${color}`,
              borderRadius: 10, padding: "14px 18px", minWidth: 200, pointerEvents: "none",
            }}>
              <div style={{ fontSize: 15, fontWeight: 700, color: "#fff", marginBottom: 8 }}>{label}</div>
              {specs.map((s, i) => (
                <div key={i} style={{ fontSize: 12, color: "#9ca3af", lineHeight: 1.7 }}>{s}</div>
              ))}
            </div>
          </Html>
        );
      })()}
    </>
  );
}

export default function Schematic3DEmbed() {
  return (
    <div className="w-full h-[480px] bg-[#0a0e1a] rounded-md border border-[#222] mb-8 relative overflow-hidden">
      <div className="absolute bottom-3 left-3 z-10 text-[10px] text-[#444] px-2 py-1" style={{ fontFamily: "'JetBrains Mono', monospace" }}>
        drag to rotate · scroll to zoom · click to inspect
      </div>
      <Canvas camera={{ position: [12, 8, 12], fov: 45 }}>
        <Suspense fallback={<Html center><span style={{ color: "#fff", fontSize: 14 }}>Loading 3D models...</span></Html>}>
          <Scene />
        </Suspense>
      </Canvas>
    </div>
  );
}
