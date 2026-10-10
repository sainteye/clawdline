#!/usr/bin/env node
// tools/check-mirror-tests.mjs [--web <dir>] [--write] — a console test asserts
// behaviour, not the shape of the code it tests.
//
// The console has no DOM test environment, so a test that needs to see inside a
// React component reads the component's own source with readFileSync and
// matches a regular expression against it. That is sound for a few things and
// worthless for most: an assertion derived from the implementation restates
// what the code already says, fails on a rename that changes nothing, and
// cannot fail on a bug. On 2026-10-10 a session was stopped while adding one of
// these to pin a branch it had just deleted; counting the rest found 576 source
// assertions, around 340 of them transcriptions of declarations, hooks, control
// flow and CSS values.
//
// So a pattern matched against source text may not contain implementation
// syntax (BANNED below), and `assert.doesNotMatch` against source needs a
// reason: a `said()` lookup, or a third argument saying why the absence
// matters. Everything else — catalog lookups, ARIA and role attributes,
// accessibility media queries, wire constants — is left alone.
//
// The baseline holds what existed when the guard went in, per file, as the
// pattern text itself so a renumbered line does not matter and a new assertion
// is not hidden by a deleted one. It may only shrink: --write rewrites it, and
// a fresh violation fails until someone either fixes it or writes it down.
//
// Exit: 0 clean, 1 a new violation, 2 could not check.
import { readFileSync, writeFileSync, existsSync } from "node:fs"
import { dirname, join } from "node:path"
import { fileURLToPath } from "node:url"
import { walk } from "./console-tests.mjs"

const here = dirname(fileURLToPath(import.meta.url))
const args = process.argv.slice(2)
const write = args.includes("--write")
const webAt = args.indexOf("--web")
const webDir = webAt >= 0 && args[webAt + 1] ? args[webAt + 1] : join(here, "..", "web")
const consoleDir = join(webDir, "console")
const baselineFile = join(consoleDir, "mirror-test-baseline.json")

// Implementation syntax: if the pattern says this, it is reading the code.
const BANNED = [
  [/\b(?:const|let|var)\s+[\\[\w]/u, "a declaration"],
  [/\buse(?:State|Effect|Callback|Memo|Ref|Context)\s*\\?\(/u, "a React hook call"],
  [/\bif\s*\\?\(|\breturn\s|\?\s*<|&&\s*<|\|\|\s*<|\}\s*else\b/u, "control flow"],
  [/[!=]==\s*["'`]|\.(?:filter|find|map|some|every|reduce)\s*\\?\(/u, "an expression from the code"],
  [/(?:cursor|color|background|padding|margin|border|font-[a-z]+|max-height|min-height|line-height|opacity|z-index|flex|grid-[a-z]+|inset|transform|box-shadow)\s*:/u, "a CSS property value"],
]
// Reasons a source assertion is about something the code does not define.
const ALLOWED = [
  /\bsaid\s*\(/u,                                   // a written-out sentence, looked up in the catalog
  /\b(?:catalogWord|catalogFormat|\w*[Ww]ord|wordCall)\s*\\?\(/u,  // a catalog key
  /aria-[a-z]+|\brole=/u,                           // an accessibility attribute
  /@media\s*\\?\(\s*prefers-/u,                     // an accessibility media query
]


// Where the file's string and template literals are. An assertion written
// inside one is a fixture, not an assertion: this guard's own test builds a
// little console out of such strings, and the first version reported them.
function quotedRanges(text) {
  const ranges = []
  for (let i = 0; i < text.length; i++) {
    const c = text[i]
    if (c === "\\") { i++; continue }
    if (c === "/" && isRegexStart(text, i)) { i = skipRegex(text, i); continue }
    if (c === '"' || c === "'" || c === "`") {
      const end = skipQuoted(text, i)
      ranges.push([i, end])
      i = end
    }
  }
  return ranges
}
const inside = (ranges, at) => ranges.some(([start, end]) => at > start && at < end)

export function sourceVariables(text) {
  const vars = new Set()
  const quoted = quotedRanges(text)
  for (const m of text.matchAll(/(?:const|let)\s+(\w+)\s*=\s*readFileSync\(([^)]*)\)/gu)) {
    if (inside(quoted, m.index)) continue
    const arg = m[2]
    if (/testdata|fixture|\.json/u.test(arg)) continue
    if (/\.tsx?|\.css|\.mjs|\.js|import\.meta\.url|new URL/u.test(arg)) vars.add(m[1])
  }
  return vars
}

// The end of the call that opens at `open`, so a pattern spanning lines is read
// whole and a third argument is visible. Brackets inside a string or a regular
// expression are text, not structure: counting them walks the end of the call
// into the next test's declaration, which is how the first attempt at this
// guard cut a test header in half.
export function callEnd(text, open) {
  let depth = 0
  for (let i = open; i < text.length; i++) {
    const c = text[i]
    if (c === "\\") { i++; continue }
    if (c === '"' || c === "'" || c === "`") { i = skipQuoted(text, i); continue }
    if (c === "/" && isRegexStart(text, i)) { i = skipRegex(text, i); continue }
    if (c === "(") depth++
    else if (c === ")") { depth--; if (!depth) return i }
  }
  return -1
}
function skipQuoted(text, start) {
  const quote = text[start]
  for (let i = start + 1; i < text.length; i++) {
    if (text[i] === "\\") { i++; continue }
    if (text[i] === quote) return i
    if (quote !== "`" && text[i] === "\n") return i - 1   // an unterminated quote is not a string
  }
  return text.length
}
// A slash opens a regular expression where a value may start: after a comma,
// an opening bracket, an operator or `return`.
function isRegexStart(text, i) {
  if (text[i + 1] === "/" || text[i + 1] === "*") return false
  for (let j = i - 1; j >= 0; j--) {
    const c = text[j]
    if (c === " " || c === "\t" || c === "\n") continue
    return "(,=:[!&|?{;+-*%<>".includes(c) || /\breturn$/u.test(text.slice(Math.max(0, j - 6), j + 1))
  }
  return true
}
function skipRegex(text, start) {
  for (let i = start + 1; i < text.length; i++) {
    if (text[i] === "\\") { i++; continue }
    if (text[i] === "[") { while (i < text.length && text[i] !== "]") { if (text[i] === "\\") i++; i++ } ; continue }
    if (text[i] === "/") return i
    if (text[i] === "\n") return start      // not a regex after all
  }
  return text.length
}
function callAt(text, open) {
  const end = callEnd(text, open)
  return end < 0 ? null : text.slice(open + 1, end)
}

export function violations(rel, text) {
  const vars = sourceVariables(text)
  if (!vars.size) return []
  const found = []
  const quoted = quotedRanges(text)
  for (const m of text.matchAll(/assert\.(match|doesNotMatch)\s*\(/gu)) {
    if (inside(quoted, m.index)) continue
    const inner = callAt(text, m.index + m[0].length - 1)
    if (!inner) continue
    const named = /^\s*(\w+)\s*,/u.exec(inner)
    if (!named || !vars.has(named[1])) continue
    const pattern = inner.slice(named[0].length).trim()
    const line = text.slice(0, m.index).split("\n").length
    if (ALLOWED.some((ok) => ok.test(pattern))) continue
    if (m[1] === "doesNotMatch" && !/,\s*["'`]/u.test(pattern)) {
      found.push({ rel, line, pattern, why: "an absence with no stated reason" })
      continue
    }
    const banned = BANNED.find(([re]) => re.test(pattern))
    if (banned) found.push({ rel, line, pattern, why: banned[1] })
  }
  return found
}

if (import.meta.url !== `file://${process.argv[1]}`) { /* imported for its parser */ } else await main()

async function main() {
let files
try {
  files = walk(consoleDir, ".", /\.test\.(ts|tsx|mts|cts|mjs|js|cjs|jsx)$/u).map((p) => p.replace(/^\.\//u, ""))
} catch (err) {
  console.error(`check-mirror-tests: could not read ${consoleDir}: ${err.message}`)
  process.exit(2)
}

const current = {}
let total = 0
for (const rel of files) {
  const found = violations(rel, readFileSync(join(consoleDir, rel), "utf8"))
  if (!found.length) continue
  total += found.length
  const counts = {}
  for (const v of found) counts[v.pattern] = (counts[v.pattern] ?? 0) + 1
  current[rel] = { count: found.length, patterns: counts }
}

if (write) {
  writeFileSync(baselineFile, JSON.stringify({ version: 1, total, files: current }, null, 2) + "\n")
  console.log(`check-mirror-tests: baseline written, ${total} assertion(s) in ${Object.keys(current).length} file(s)`)
  process.exit(0)
}

let baseline
try {
  baseline = existsSync(baselineFile) ? JSON.parse(readFileSync(baselineFile, "utf8")) : { files: {} }
} catch (err) {
  console.error(`check-mirror-tests: could not read ${baselineFile}: ${err.message}`)
  process.exit(2)
}

let failed = false
for (const [rel, now] of Object.entries(current)) {
  const was = baseline.files?.[rel]?.patterns ?? {}
  for (const [pattern, count] of Object.entries(now.patterns)) {
    const allowed = was[pattern] ?? 0
    if (count <= allowed) continue
    failed = true
    const where = violations(rel, readFileSync(join(consoleDir, rel), "utf8")).find((v) => v.pattern === pattern)
    console.error(`check-mirror-tests: web/console/${rel}:${where?.line ?? "?"} asserts on ${where?.why ?? "implementation syntax"}`)
    console.error(`    ${pattern.split("\n")[0].slice(0, 120)}`)
  }
}
if (failed) {
  console.error("  A source-text assertion may not restate the code: assert what a person would notice.")
  console.error("  Pull the logic into a function and test that, or match a catalog lookup, an ARIA attribute, or a said() sentence.")
  console.error("  An assert.doesNotMatch on source needs a third argument saying why the absence matters.")
  process.exit(1)
}
const shrunk = (baseline.total ?? 0) - total
console.log(`check-mirror-tests: ${total} source assertion(s) left in ${Object.keys(current).length} file(s)` +
  (shrunk > 0 ? `, ${shrunk} fewer than the baseline (run --write to record it)` : ""))
}
