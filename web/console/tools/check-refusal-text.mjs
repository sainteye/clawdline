import { spawnSync } from "node:child_process"
import fs from "node:fs"
import os from "node:os"
import path from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"

const tools = path.dirname(fileURLToPath(import.meta.url))
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "clawdline-refusal-text-"))
try {
  const output = path.join(scratch, "refusal-text.test.mjs")
  await build({
    entryPoints: [path.resolve(tools, "../src/refusals/refusal-text.test.ts")],
    outfile: output,
    bundle: true,
    platform: "node",
    format: "esm",
    logLevel: "silent",
  })
  const test = spawnSync(process.execPath, ["--test", output], { stdio: "inherit" })
  if (test.error) throw test.error
  if (test.status !== 0) process.exitCode = test.status || 1
} finally {
  fs.rmSync(scratch, { recursive: true, force: true })
}
