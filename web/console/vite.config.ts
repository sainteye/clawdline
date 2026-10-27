import { resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig, loadEnv } from "vite"
import react from "@vitejs/plugin-react"
import { validateHostedDeclaration } from "./tools/hosted-declaration.mjs"

const here = fileURLToPath(new URL(".", import.meta.url))

// The console is served by the daemon in a release and by Vite in development.
// In development every /v1 call is proxied to the daemon rather than pointed at
// it by an absolute URL, so the code that runs in the browser is the same code
// either way and no build flag decides where the data comes from.
const daemon = process.env.CLAWDLINE_NEXT_ORIGIN ?? "http://127.0.0.1:7727"

export default defineConfig(({ mode }) => {
  const declared = process.env.VITE_HOSTED_CONSOLE ?? loadEnv(mode, here, "VITE_").VITE_HOSTED_CONSOLE
  validateHostedDeclaration(declared)
  return {
    plugins: [react()],
    // Hosted routes live below the origin root (/admin/accounts, for example),
    // while a daemon-served console still needs relative asset URLs.
    base: declared ? "/" : "./",
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
    // The console owns Settings. Only the input bar has a separate document,
    // because its floating native window must load without console navigation.
    build: {
      outDir: "dist",
      emptyOutDir: true,
      rollupOptions: {
        input: {
          main: resolve(here, "index.html"),
          bar: resolve(here, "bar.html"),
        },
      },
    },
  }
})
