import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The board is served as static assets from the Worker (and from `lineup serve` in development), so the build
// is a plain directory of files. `VITE_DATA_BASE` is where the index lives: empty means same origin, which is
// what `lineup serve` gives; the edge build points at the R2 bucket.
export default defineConfig({
  plugins: [react()],
  server: {
    // :3000 is the convention for a single-port dev app, and `dev-server.messer.wtf` proxies to it — a dev
    // server on any other port is unreachable at the URL everyone opens. Strict, so a second project's dev
    // server fails loudly instead of auto-bumping to 3001 while the tunnel still points at 3000.
    port: 3000,
    strictPort: true,
    host: true,
    // Vite answers an unknown Host with 403 "Blocked request", which looks exactly like a broken app behind
    // Caddy. Every dev hostname the tunnel can send has to be here.
    allowedHosts: [".messer.wtf", ".local"],
    // `npm run dev` needs the index on its own origin. `lineup serve` holds it, so the dev server proxies the
    // index paths rather than the app growing a second data source (and rather than a CORS policy being
    // needed for localhost on the bucket). Portraits and the ICS link are on the same origin in
    // production, so they are proxied too — otherwise a face 404s in dev only.
    proxy: {
      "/manifest.json": "http://127.0.0.1:8016",
      "/towns.json": "http://127.0.0.1:8016",
      "/towns": "http://127.0.0.1:8016",
      "/artists": "http://127.0.0.1:8016",
      "/calendar": "http://127.0.0.1:8016",
      // The artist picker asks the same edge the live board asks; `lineup serve` answers the route too.
      "/api": "http://127.0.0.1:8016",
    },
  },
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsInlineLimit: 2048,
  },
});
