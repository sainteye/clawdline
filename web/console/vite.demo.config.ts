import { resolve } from "node:path"
import { fileURLToPath } from "node:url"
import { defineConfig } from "vite"
import { readFileSync } from "node:fs"
import react from "@vitejs/plugin-react"

const here = fileURLToPath(new URL(".", import.meta.url))
const storyPath = process.env.CLAWDLINE_DEMO_STORY ? resolve(process.env.CLAWDLINE_DEMO_STORY) : resolve(here, "demo/story.json")

// A separate entry: no demo adapter is included in the installed or hosted console.
export default defineConfig({
  root: resolve(here, "demo"),
  base: "./",
  publicDir: false,
  resolve: { alias: [{ find: "./story.json", replacement: storyPath }] },
  plugins: [react(), { name: "homepage-story", generateBundle() { this.emitFile({ type: "asset", fileName: "story.json", source: readFileSync(storyPath, "utf8") }) } }],
  build: { outDir: resolve(here, "dist/demo"), emptyOutDir: true },
})
