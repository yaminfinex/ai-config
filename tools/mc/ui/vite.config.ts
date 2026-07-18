/// <reference types="vitest/config" />
import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The SPA mounts under /ui/ on the Go binary (go:embed). In dev, Vite owns
// the page and proxies /api to the Go process on its default port.
export default defineConfig({
  base: "/ui/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "src") },
  },
  server: {
    proxy: { "/api": "http://127.0.0.1:8390" },
  },
  test: {
    include: ["src/**/*.test.ts"],
    environment: "node",
  },
});
