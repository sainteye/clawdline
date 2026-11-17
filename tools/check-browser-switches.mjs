#!/usr/bin/env node
// tools/check-browser-switches.mjs — ensures all Chromium/Chrome launches disable OmniboxEverywhere.
// Exit: 0 clean, 1 a violation, 2 could not check.
import { readFileSync, readdirSync, statSync } from "node:fs"
import { join, dirname } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, "..")
const EXTENSIONS = new Set([".go", ".ts", ".js", ".mjs"])
const IGNORE_DIRS = new Set(["node_modules", ".git", "dist", "build", "vendor", "testdata", "catalogs"])

const REQUIRED_FLAG = "--disable-features=OmniboxEverywhere"

function walk(dir, out = []) {
  try {
    for (const entry of readdirSync(dir)) {
      if (IGNORE_DIRS.has(entry)) continue
      const full = join(dir, entry)
      const st = statSync(full)
      if (st.isDirectory()) {
        walk(full, out)
      } else {
        const ext = entry.slice(entry.lastIndexOf("."))
        if (EXTENSIONS.has(ext)) out.push(full)
      }
    }
  } catch (err) {
    console.error(`check-browser-switches: could not read ${dir}: ${err.message}`)
    process.exit(2)
  }
  return out
}

const files = walk(root)
const violations = []
let verifiedCount = 0

for (const file of files) {
  if (file.endsWith("check-browser-switches.mjs")) continue
  const content = readFileSync(file, "utf8")
  const lines = content.split("\n")
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    // Identify lines where Chromium/Chrome is launched
    const isLaunch =
      (line.includes("spawn(") && (line.includes("chrome") || line.includes("browser") || line.includes("executable"))) ||
      (line.includes("exec.Command(") && (line.includes("chromium") || line.includes("o.Browser"))) ||
      (line.includes("args :=") && line.includes("--user-data-dir="))

    if (!isLaunch) continue

    // Inspect surrounding context (up to 15 lines before and after) for arguments
    const window = lines.slice(Math.max(0, i - 15), Math.min(lines.length, i + 15)).join("\n")
    if (window.includes("--version") || window.includes("kill") || window.includes("-test.run") || window.includes("go test")) {
      continue
    }

    if (!window.includes(REQUIRED_FLAG)) {
      const rel = file.slice(root.length + 1)
      violations.push(`${rel}:${i + 1}`)
    } else {
      verifiedCount++
    }
  }
}

if (violations.length > 0) {
  console.error(`check-browser-switches: ${violations.length} Chromium launch invocation(s) missing ${REQUIRED_FLAG}:`)
  for (const v of violations) {
    console.error(`  ${v}`)
  }
  process.exit(1)
}

console.log(`check-browser-switches: verified ${verifiedCount} Chromium launch invocation(s) cleanly disable OmniboxEverywhere`)
process.exit(0)
