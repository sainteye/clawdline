import { resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"

const here = fileURLToPath(new URL(".", import.meta.url))

// A separate entry: no demo adapter is included in the installed or hosted console.
export default defineConfig({
  root: resolve(here, "demo"),
  base: "./",
  publicDir: false,
  plugins: [react()],
  build: { outDir: resolve(here, "dist/demo"), emptyOutDir: true },
})
