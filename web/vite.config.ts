import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "/ui/",
  resolve: {
    alias: { "@": path.resolve(__dirname, "src") },
  },
  build: {
    outDir: path.resolve(__dirname, "../internal/web/static"),
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/admin": "http://localhost:8080",
      "/v1": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});
