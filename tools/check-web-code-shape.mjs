#!/usr/bin/env node
// tools/check-web-code-shape.mjs [--web <dir>] [--tighten] — the console's
// TypeScript keeps its layers and its functions reviewable.
//
// The same two ratchets as internal/config/codeshape_test.go applies to Go,
// for the same reason: work added one small increment at a time piles every
// new case into the nearest function, and reaches across a layer because that
// is the shortest path.
//
// Layers (docs/plan.md §3.1): `contract` holds generated types and imports
// nothing outside itself. `core` is shared with a future React Native app, so
// it imports only itself and `@clawdline/contract`: no React, no console code,
// no stylesheet or bundler query import, no `import.meta`. Its DOM-free `lib`
// is enforced separately by type-checking it with its own tsconfig. There were
// no violations when this guard was written, so there is no layer baseline.
//
// Functions: a function with more than MAX_STATEMENTS statements or
// MAX_BRANCHES branches (if, case, loops, `?:`) is measured from its outermost
// function, closures included, so a component's handlers count toward the
// component. Statements, not lines, are counted: JSX and literal tables are
// expressions. Those over a limit when the guard was written are in
// web/function-size-baseline.txt and may not grow; a shrunken or vanished one
// must be lowered or removed. --tighten rewrites the baseline but only removes
// entries and lowers numbers, so growth is a hand edit visible in review.
//
// Production files only: *.test.*, *.e2e.* and generated files are skipped.
//
// Exit: 0 clean, 1 a violation, 2 could not check.
import { existsSync, readFileSync, readdirSync, writeFileSync } from "node:fs"
import { createRequire } from "node:module"
import { dirname, join, relative, resolve, sep } from "node:path"
import { fileURLToPath } from "node:url"

export const MAX_STATEMENTS = 100
export const MAX_BRANCHES = 40

const here = dirname(fileURLToPath(import.meta.url))
const SOURCE = /\.(?:ts|tsx|mts|js|jsx|mjs)$/
const NOT_PRODUCTION = /\.(?:test|e2e)\.[cm]?[jt]sx?$|\.d\.ts$/
const SKIP_DIRS = new Set(["node_modules", "dist", "build", "testdata", "fixtures"])

export function loadTypeScript(webDir) {
  const require = createRequire(join(webDir, "package.json"))
  return require("typescript")
}

/** The workspace a web-relative path belongs to: contract, core, console. */
export function workspaceOf(webRelative) {
  return webRelative.split("/")[0]
}

/** Why one module specifier is not allowed from a file, or "". */
export function importProblem(fromWebRelative, specifier) {
  const ws = workspaceOf(fromWebRelative)
  if (ws !== "core" && ws !== "contract") return ""
  if (specifier.startsWith(".")) {
    const target = join(dirname(fromWebRelative), specifier).split(sep).join("/")
    if (target !== ws && !target.startsWith(ws + "/")) return `reaches outside ${ws}/ to ${target}`
    if (/\.(?:css|scss|svg|png)$|\?/.test(specifier)) return `imports a bundler asset (${specifier})`
    return ""
  }
  if (ws === "core" && (specifier === "@clawdline/contract" || specifier.startsWith("@clawdline/contract/"))) return ""
  return ws === "core"
    ? `imports ${specifier}; core imports only itself and @clawdline/contract`
    : `imports ${specifier}; contract imports nothing outside itself`
}

function isFunctionLike(ts, n) {
  return ts.isFunctionDeclaration(n) || ts.isFunctionExpression(n) || ts.isArrowFunction(n) ||
    ts.isMethodDeclaration(n) || ts.isConstructorDeclaration(n) || ts.isGetAccessorDeclaration(n) ||
    ts.isSetAccessorDeclaration(n)
}

function nameOf(ts, n) {
  if (n.name && (ts.isIdentifier(n.name) || ts.isStringLiteral(n.name) || ts.isPrivateIdentifier(n.name))) return n.name.text
  if (ts.isConstructorDeclaration(n)) return "constructor"
  return ""
}

/** A stable name for an outermost function: its own name, else the names it is assigned to. */
function functionKey(ts, fn) {
  const parts = []
  const own = nameOf(ts, fn)
  if (own) parts.push(own)
  for (let p = fn.parent; p && !ts.isSourceFile(p); p = p.parent) {
    if (ts.isClassDeclaration(p) || ts.isClassExpression(p)) parts.push(p.name ? p.name.text : "class")
    else if (ts.isVariableDeclaration(p) && ts.isIdentifier(p.name)) parts.push(p.name.text)
    else if ((ts.isPropertyAssignment(p) || ts.isPropertyDeclaration(p)) && p.name && ts.isIdentifier(p.name)) parts.push(p.name.text)
    else if (ts.isExportAssignment(p)) parts.push("default")
  }
  if (parts.length === 0) return "anonymous"
  return parts.reverse().filter((x, i, a) => i === 0 || x !== a[i - 1]).join(".")
}

/** Counts statements and branches inside a function, closures included. */
export function measureFunction(ts, fn) {
  let statements = 0
  let branches = 0
  const visit = (n) => {
    if (n.kind >= ts.SyntaxKind.FirstStatement && n.kind <= ts.SyntaxKind.LastStatement) statements++
    if (ts.isIfStatement(n) || ts.isCaseClause(n) || ts.isForStatement(n) || ts.isForInStatement(n) ||
      ts.isForOfStatement(n) || ts.isWhileStatement(n) || ts.isDoStatement(n) || ts.isConditionalExpression(n)) branches++
    ts.forEachChild(n, visit)
  }
  if (fn.body) ts.forEachChild(fn.body, visit)
  // A concise arrow body is one expression: it can still hold `?:` branches.
  if (fn.body && !ts.isBlock(fn.body)) visit(fn.body)
  return { statements, branches }
}

export function isGenerated(text) {
  return /^\/\/ Code generated .* DO NOT EDIT\./m.test(text.slice(0, 500))
}

/**
 * Reads one source text. Returns the import problems and every outermost
 * function keyed by "path<TAB>name"; a repeated name gets "#2", "#3".
 */
export function scanSource(ts, repoRelative, webRelative, text) {
  const kind = /\.tsx$|\.jsx$/.test(repoRelative) ? ts.ScriptKind.TSX : /\.m?js$/.test(repoRelative) ? ts.ScriptKind.JS : ts.ScriptKind.TS
  const sf = ts.createSourceFile(repoRelative, text, ts.ScriptTarget.Latest, true, kind)
  const imports = []
  const functions = new Map()
  const seen = new Map()
  const where = (n) => `${repoRelative}:${sf.getLineAndCharacterOfPosition(n.getStart(sf)).line + 1}`
  const checkSpecifier = (n, lit) => {
    if (!lit || !ts.isStringLiteralLike(lit)) return
    const why = importProblem(webRelative, lit.text)
    if (why) imports.push(`${where(n)} ${why}`)
  }
  const ws = workspaceOf(webRelative)
  // inside is true below an outermost function: nested functions are counted
  // as part of it, and the walk continues only to read imports.
  const visit = (n, inside) => {
    if ((ts.isImportDeclaration(n) || ts.isExportDeclaration(n)) && n.moduleSpecifier) checkSpecifier(n, n.moduleSpecifier)
    else if (ts.isImportTypeNode(n) && ts.isLiteralTypeNode(n.argument)) checkSpecifier(n, n.argument.literal)
    else if (ts.isCallExpression(n) && (n.expression.kind === ts.SyntaxKind.ImportKeyword ||
      (ts.isIdentifier(n.expression) && n.expression.text === "require"))) checkSpecifier(n, n.arguments[0])
    else if (ts.isMetaProperty(n) && n.keywordToken === ts.SyntaxKind.ImportKeyword && (ws === "core" || ws === "contract")) {
      imports.push(`${where(n)} uses import.meta, which is bundler and host specific`)
    }
    const outermost = !inside && isFunctionLike(ts, n) && n.body
    if (outermost) {
      let name = functionKey(ts, n)
      const count = (seen.get(name) || 0) + 1
      seen.set(name, count)
      if (count > 1) name += "#" + count
      functions.set(`${repoRelative}\t${name}`, measureFunction(ts, n))
    }
    ts.forEachChild(n, (c) => visit(c, inside || Boolean(outermost)))
  }
  visit(sf, false)
  return { imports, functions }
}

function walk(dir, out) {
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.startsWith(".")) continue
    const path = join(dir, entry.name)
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(entry.name)) walk(path, out)
    } else if (SOURCE.test(entry.name) && !NOT_PRODUCTION.test(entry.name)) {
      out.push(path)
    }
  }
}

export function scanWeb(ts, webDir) {
  const repo = resolve(webDir, "..")
  const files = []
  for (const ws of ["contract", "core", "console"]) {
    const src = join(webDir, ws, "src")
    if (existsSync(src)) walk(src, files)
  }
  const imports = []
  const functions = new Map()
  let scanned = 0
  for (const path of files.sort()) {
    const text = readFileSync(path, "utf8")
    if (isGenerated(text)) continue
    scanned++
    const r = scanSource(ts, relative(repo, path).split(sep).join("/"), relative(webDir, path).split(sep).join("/"), text)
    imports.push(...r.imports)
    for (const [k, v] of r.functions) functions.set(k, v)
  }
  return { imports, functions, scanned }
}

const over = (s) => s.statements > MAX_STATEMENTS || s.branches > MAX_BRANCHES

/** Compares measured functions with baseline lines; tightened only removes and lowers. */
export function functionProblems(found, baseline) {
  const problems = []
  const tightened = []
  const listed = new Set()
  for (const line of baseline) {
    const f = line.split("\t")
    const st = Number(f[2])
    const br = Number(f[3])
    if (f.length !== 4 || !Number.isInteger(st) || !Number.isInteger(br)) {
      problems.push(`a function baseline line is not path<TAB>name<TAB>statements<TAB>branches: ${line}`)
      continue
    }
    const key = f[0] + "\t" + f[1]
    const at = `${f[0]} ${f[1]}`
    if (listed.has(key)) { problems.push(`listed twice in the function baseline: ${at}`); continue }
    listed.add(key)
    const now = found.get(key)
    if (!now) { problems.push(`the function baseline names a function that is gone, take its line out: ${at}`); continue }
    if (!over(now)) { problems.push(`${at} now fits the limits (${now.statements} statements, ${now.branches} branches), take its line out`); continue }
    if (now.statements > st || now.branches > br) {
      problems.push(`${at} grew to ${now.statements} statements and ${now.branches} branches; its baseline is ${st} and ${br}. Split out the new case instead of adding it here`)
    } else if (now.statements < st || now.branches < br) {
      problems.push(`${at} shrank to ${now.statements} statements and ${now.branches} branches; lower its baseline from ${st} and ${br} (--tighten)`)
    }
    tightened.push(`${f[0]}\t${f[1]}\t${Math.min(st, now.statements)}\t${Math.min(br, now.branches)}`)
  }
  const fresh = []
  for (const [key, s] of found) {
    if (over(s) && !listed.has(key)) {
      fresh.push(`${key.replace("\t", " ")} has ${s.statements} statements and ${s.branches} branches; the limits are ${MAX_STATEMENTS} and ${MAX_BRANCHES}. Split it into functions that can each be read on their own`)
    }
  }
  return { problems: problems.concat(fresh.sort()), tightened }
}

export function readBaseline(path) {
  const header = []
  const lines = []
  let inHeader = true
  for (const line of readFileSync(path, "utf8").split("\n")) {
    if (line.startsWith("#")) { if (inHeader) header.push(line); continue }
    inHeader = false
    if (line.trim()) lines.push(line)
  }
  return { header, lines }
}

function main() {
  const args = process.argv.slice(2)
  const webAt = args.indexOf("--web")
  const webDir = resolve(webAt >= 0 && args[webAt + 1] ? args[webAt + 1] : join(here, "..", "web"))
  const baselineFile = join(webDir, "function-size-baseline.txt")
  let ts
  try {
    ts = loadTypeScript(webDir)
  } catch (err) {
    console.error(`check-web-code-shape: cannot load typescript from ${webDir} (${err.message}); run: cd web && npm run deps`)
    return 2
  }
  const { imports, functions, scanned } = scanWeb(ts, webDir)
  if (scanned < 300 || functions.size < 2000) {
    console.error(`check-web-code-shape: read ${scanned} files and ${functions.size} functions; it is not reading the tree`)
    return 2
  }
  if (!existsSync(baselineFile)) {
    console.error(`check-web-code-shape: ${baselineFile} is missing`)
    return 2
  }
  const { header, lines } = readBaseline(baselineFile)
  const { problems, tightened } = functionProblems(functions, lines)
  const tighten = args.includes("--tighten")
  if (tighten) {
    writeFileSync(baselineFile, header.join("\n") + "\n" + tightened.sort().join("\n") + "\n")
    console.log(`check-web-code-shape: rewrote ${relative(process.cwd(), baselineFile)} with ${tightened.length} functions`)
  }
  const all = imports.map((p) => "layer: " + p).concat(tighten ? [] : problems)
  for (const p of all) console.error(p)
  if (all.length) {
    console.error(`check-web-code-shape: ${all.length} problem(s); see the header of tools/check-web-code-shape.mjs`)
    return 1
  }
  console.log(`check-web-code-shape: ${scanned} files, ${functions.size} functions, ${lines.length} on the size baseline`)
  return 0
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  process.exitCode = main()
}
