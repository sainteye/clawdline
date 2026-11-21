#!/usr/bin/env node
// Check Chromium launch switches, including keychain isolation for custom profiles.
// Exit: 0 clean, 1 a violation, 2 could not check.
import { readFileSync, readdirSync, statSync } from "node:fs"
import { join, dirname } from "node:path"
import { fileURLToPath } from "node:url"

const here = dirname(fileURLToPath(import.meta.url))
const root = join(here, "..")
const EXTENSIONS = new Set([".go", ".ts", ".js", ".mjs"])
const IGNORE_DIRS = new Set(["node_modules", ".git", "dist", "build", "vendor", "testdata", "catalogs"])

const OMNIBOX_FLAG = "--disable-features=OmniboxEverywhere"
const PROFILE_ARGUMENT = /(?:^|[\[,{])\s*["'`]--user-data-dir(?:=|["'`])/
const MOCK_KEYCHAIN_ARGUMENT = /["'`]--use-mock-keychain["'`]/

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
let profileCount = 0

for (const file of files) {
  if (file.endsWith("check-browser-switches.mjs")) continue
  const content = readFileSync(file, "utf8")
  const lines = content.split("\n")
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i]
    const location = `${file.slice(root.length + 1)}:${i + 1}`

    // Custom profiles here belong to the isolated launcher or disposable test tools.
    // Check the argument even when a version probe or cleanup is nearby.
    if (PROFILE_ARGUMENT.test(line)) {
      const args = lines.slice(Math.max(0, i - 4), i + 5).join("\n")
      if (MOCK_KEYCHAIN_ARGUMENT.test(args)) profileCount++
      else violations.push(`${location}: missing --use-mock-keychain for custom Chrome profile`)
    }

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

    if (!window.includes(OMNIBOX_FLAG)) {
      violations.push(`${location}: missing ${OMNIBOX_FLAG}`)
    } else {
      verifiedCount++
    }
  }
}

if (violations.length > 0) {
  console.error(`check-browser-switches: ${violations.length} Chromium launch switch violation(s):`)
  for (const v of violations) {
    console.error(`  ${v}`)
  }
  process.exit(1)
}

console.log(`check-browser-switches: verified ${verifiedCount} Chromium launch invocation(s) cleanly disable OmniboxEverywhere`)
console.log(`check-browser-switches: verified ${profileCount} custom Chrome profile argument list(s) use a mock keychain`)
process.exit(0)
