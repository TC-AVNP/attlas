import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Base path is /petboard/ because the app is served behind Caddy at that
// prefix. Every asset URL and every react-router route is rooted at it.
export default defineConfig({
  base: "/petboard/",
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
  },
});
