import { resolve } from "node:path"

import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

// The console is served by the daemon in a release and by Vite in development.
// In development every /v1 call is proxied to the daemon rather than pointed at
// it by an absolute URL, so the code that runs in the browser is the same code
// either way and no build flag decides where the data comes from.
const daemon = process.env.CLAWDLINE_NEXT_ORIGIN ?? "http://127.0.0.1:7727"

export default defineConfig({
  plugins: [react()],
  base: "./",
  server: {
    port: 5273,
    proxy: {
      "/v1": {
        target: daemon,
        changeOrigin: true,
        // Server-sent events must not be buffered on the way through, or the
        // fleet list looks frozen in development and nowhere else.
        configure: (proxy) => {
          // The daemon's gate compares Origin with its own scheme, host and
          // port, and the browser names Vite's. Only the Origin is rewritten:
          // Sec-Fetch-Site is still the browser's own, so a page from another
          // origin is still refused a write.
          proxy.on("proxyReq", (proxyReq) => {
            if (proxyReq.getHeader("origin")) proxyReq.setHeader("origin", daemon)
          })
          proxy.on("proxyRes", (proxyRes) => {
            if (proxyRes.headers["content-type"]?.includes("text/event-stream")) {
              proxyRes.headers["cache-control"] = "no-cache"
            }
          })
        },
      },
    },
  },
  // Two documents. index.html is the console — the Swift app's web page,
  // replicated. settings.html is the native settings window, which was never a
  // web page there and is one here so that the Linux and Windows shells can
  // open the same window (docs/shell-bridge.md).
  build: {
    outDir: "dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        index: resolve(import.meta.dirname, "index.html"),
        settings: resolve(import.meta.dirname, "settings.html"),
      },
    },
  },
})
