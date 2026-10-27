import { execFileSync } from "node:child_process"
import { readFileSync, readdirSync, writeFileSync } from "node:fs"
import { join } from "node:path"
import { fileURLToPath } from "node:url"
import { build } from "vite"
import { productionCloud, validateHostedDeclaration } from "./hosted-declaration.mjs"
import { validateHostedBundle } from "./hosted-bundle.mjs"

const declared = JSON.stringify(productionCloud)
validateHostedDeclaration(declared)
const root = fileURLToPath(new URL("../", import.meta.url))
const git = (...args) => execFileSync("git", ["-C", root, ...args], { encoding: "utf8" }).trim()
if (git("status", "--porcelain")) throw new Error("Build the hosted console from a clean, committed worktree.")
const stamp = git("rev-parse", "HEAD")
const committed_at = git("show", "-s", "--format=%cI", stamp)
process.env.VITE_HOSTED_CONSOLE = declared
await build({ root, configFile: join(root, "vite.config.ts") })
const assets = join(root, "dist", "assets")
const main = readdirSync(assets).filter(name => /^main-.*\.js$/u.test(name))
if (main.length !== 1) throw new Error("The hosted build must contain exactly one main bundle.")
validateHostedBundle(readFileSync(join(assets, main[0]), "utf8"))
if (git("rev-parse", "HEAD") !== stamp || git("status", "--porcelain")) {
  throw new Error("The source changed during the hosted build; do not deploy its output.")
}
writeFileSync(join(root, "dist", "BUILD.json"), JSON.stringify({ stamp, committed_at }) + "\n")
console.log(`Hosted production configuration and source verified: ${stamp}`)
