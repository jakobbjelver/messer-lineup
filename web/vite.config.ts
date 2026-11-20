import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The board is served as static assets from the Worker (and from `lineup serve` in development), so the build
// is a plain directory of files. `VITE_DATA_BASE` is where the index lives: empty means same origin, which is
// what `lineup serve` gives; the edge build points at the R2 bucket.
export default defineConfig({
  plugins: [react()],
  server: {
    // `npm run dev` needs the index on its own origin. `lineup serve` holds it, so the dev server proxies the
    // three index paths rather than the app growing a second data source (and rather than a CORS policy being
    // needed for localhost on the bucket).
    proxy: {
      "/manifest.json": "http://127.0.0.1:8016",
      "/towns.json": "http://127.0.0.1:8016",
      "/towns": "http://127.0.0.1:8016",
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
