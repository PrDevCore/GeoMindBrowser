import { defineConfig, type ProxyOptions } from "vite";

// The control plane is same-origin behind a proxy so the launcher never holds a
// cross-origin API base and the bearer token never leaves this origin.
const proxy: Record<string, ProxyOptions> = {
  "/v1": { target: "http://127.0.0.1:8080", changeOrigin: false },
  "/healthz": { target: "http://127.0.0.1:8080", changeOrigin: false },
};

export default defineConfig({
  server: { port: 5173, proxy },
  // `vite preview` serves the built bundle and is what the end-to-end test drives.
  preview: { port: 4173, strictPort: true, host: "127.0.0.1", proxy },
  build: {
    outDir: "dist",
    target: "es2022",
  },
});
