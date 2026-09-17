import { resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

const here = fileURLToPath(new URL(".", import.meta.url))

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
  build: {
    outDir: "dist",
    emptyOutDir: true,
    // Two documents, not one. `index.html` is the console; `bar.html` is the
    // input bar, which the native shell loads into a borderless window of its
    // own (see src/bar/ and docs/shell-bridge.md). It is a second entry rather
    // than a page inside the console because the console's own chrome — the
    // header, the drawer, twenty-nine stylesheets — is exactly what a card
    // floating over somebody's terminal must not have, and because a panel
    // summoned by a key should not be waiting on the session list's bundle.
    rollupOptions: {
      input: {
        main: resolve(here, "index.html"),
        bar: resolve(here, "bar.html"),
      },
    },
  },
})
