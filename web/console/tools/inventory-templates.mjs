// Snapshot remaining interpolated Chinese copy after fixed literals are catalogued.
import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const entries = []
function walk(directory) {
  for (const item of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, item.name)
    if (item.isDirectory()) {
      if (item.name !== "legacy") walk(file)
      continue
    }
    if (!/\.tsx?$/u.test(file) || /\.(test|e2e)\.tsx?$/u.test(file)) continue
    const source = fs.readFileSync(file, "utf8")
    const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
    function visit(node) {
      if (ts.isTemplateExpression(node)) {
        const chunks = [node.head.text, ...node.templateSpans.map((span) => span.literal.text)]
        if (chunks.some((chunk) => /[\u3400-\u9fff]/u.test(chunk))) {
          const name = path.relative(root, file)
          const format = chunks.map((chunk, index) => chunk + (index < chunks.length - 1 ? `{arg${index}}` : "")).join("")
          const id = "template." + crypto.createHash("sha256").update(name + "\0" + format).digest("hex").slice(0, 12)
          const other = ts.isConditionalExpression(node.parent) ? node.parent.whenTrue === node ? node.parent.whenFalse : node.parent.whenTrue : null
          const english = other && ts.isTemplateExpression(other) &&
            ![other.head.text, ...other.templateSpans.map((span) => span.literal.text)].some((chunk) => /[\u3400-\u9fff]/u.test(chunk))
            ? [other.head.text, ...other.templateSpans.map((span) => span.literal.text)].map((chunk, index) => chunk + (index < other.templateSpans.length ? `{arg${index}}` : "")).join("") : ""
          entries.push({ id, file: name, line: ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1,
            "zh-Hant": format, en: english, expressions: node.templateSpans.map((span) => span.expression.getText(ast)),
            context: source.slice(Math.max(0, node.pos - 55), Math.min(source.length, node.end + 55)).replace(/\s+/gu, " ") })
          return
        }
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
  }
}
walk(path.join(root, "src"))
if (process.argv.includes("--write")) fs.writeFileSync(path.join(root, "template-inventory.json"), JSON.stringify(entries, null, 2) + "\n")
console.log(JSON.stringify({ occurrences: entries.length, keys: new Set(entries.map((entry) => entry.id)).size, pairedEnglish: entries.filter((entry) => entry.en).length }))
