// Inventory direct JSX copy and accessible-name attributes before replacing it.
import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const src = path.join(root, "src")
const files = []
function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== "legacy") walk(file)
    } else if (file.endsWith(".tsx") && !/\.(test|e2e)\.tsx$/.test(file)) files.push(file)
  }
}
walk(src)

const excluded = new Set(["clawdline", "Clawdline", "CPU", "swap", "SKILL.md", "clawdline cloud pair"])
const normalize = (value) => value.trim().replace(/\s+/g, " ")
const copy = (value) => /[A-Za-z\u3400-\u9fff]/u.test(value) && !excluded.has(value)
const entries = []
for (const file of files.sort()) {
  const source = fs.readFileSync(file, "utf8")
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  function visit(node) {
    let value = ""
    let kind = ""
    if (ts.isJsxText(node)) {
      value = normalize(node.getText(ast))
      kind = "jsx"
    } else if (ts.isJsxAttribute(node) && ["aria-label", "title", "placeholder", "alt"].includes(node.name.text) && node.initializer && ts.isStringLiteral(node.initializer)) {
      value = normalize(node.initializer.text)
      kind = node.name.text
    }
    if (value && copy(value)) {
      const id = "inline." + crypto.createHash("sha256").update(value).digest("hex").slice(0, 12)
      entries.push({ id, file: path.relative(root, file), line: ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1, kind, value })
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
}

if (process.argv.includes("--write")) {
  fs.writeFileSync(path.join(root, "visible-inventory.json"), JSON.stringify(entries, null, 2) + "\n")
}
console.log(JSON.stringify({ occurrences: entries.length, keys: new Set(entries.map((entry) => entry.id)).size, files: new Set(entries.map((entry) => entry.file)).size }))
if (process.argv.includes("--check") && entries.length) {
  for (const entry of entries) console.error(`${entry.file}:${entry.line}: ${entry.kind}: ${entry.value}`)
  process.exitCode = 1
}
