import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // Static output — this site has no backend. Drop dist/ on any host.
  build: { outDir: "dist", sourcemap: false },
});
