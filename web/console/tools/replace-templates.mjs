// One-shot conversion of reviewed template expressions to catalog formats.
import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const candidates = JSON.parse(fs.readFileSync(path.join(root, "template-candidates.json"), "utf8"))
const inventory = JSON.parse(fs.readFileSync(path.join(root, "template-inventory.json"), "utf8"))
const files = new Set(inventory.map((entry) => entry.file))
let replaced = 0
for (const name of files) {
  const file = path.join(root, name)
  let source = fs.readFileSync(file, "utf8")
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, name.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  const edits = []
  function add(node, replacement) {
    edits.push({ start: node.getStart(ast), end: node.end, replacement })
    replaced++
  }
  function template(node) {
    const chunks = [node.head.text, ...node.templateSpans.map((span) => span.literal.text)]
    const format = chunks.map((chunk, index) => chunk + (index < chunks.length - 1 ? `{arg${index}}` : "")).join("")
    const id = "template." + crypto.createHash("sha256").update(name + "\0" + format).digest("hex").slice(0, 12)
    return { id, chinese: chunks.some((chunk) => /[\u3400-\u9fff]/u.test(chunk)) }
  }
  function formatCall(id, node) {
    return `catalogFormat("template", ${JSON.stringify(id.slice(9))}, [${node.templateSpans.map((span) => span.expression.getText(ast)).join(", ")}])`
  }
  function visit(node) {
    if (ts.isConditionalExpression(node) && ts.isTemplateExpression(node.whenTrue) && ts.isTemplateExpression(node.whenFalse)) {
      const a = template(node.whenTrue)
      const b = template(node.whenFalse)
      const selected = a.chinese && !b.chinese ? node.whenTrue : b.chinese && !a.chinese ? node.whenFalse : null
      if (selected) {
        const { id } = template(selected)
        if (candidates[id]?.include) { add(node, formatCall(id, selected)); return }
      }
    }
    if (ts.isTemplateExpression(node)) {
      const { id, chinese } = template(node)
      if (chinese && candidates[id]?.include) { add(node, formatCall(id, node)); return }
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
  if (!edits.length) continue
  for (const edit of edits.sort((a, b) => b.start - a.start)) source = source.slice(0, edit.start) + edit.replacement + source.slice(edit.end)
  const relative = path.relative(path.dirname(file), path.join(root, "src/catalog")).replaceAll(path.sep, "/")
  const specifier = (relative.startsWith(".") ? relative : `./${relative}`) + ".js"
  if (!source.match(/import\s*\{[^}]*\bcatalogFormat\b[^}]*\}\s*from\s*["'][^"']*catalog\.js["']/u)) source = `import { catalogFormat } from ${JSON.stringify(specifier)}\n` + source
  fs.writeFileSync(file, source)
}
console.log(JSON.stringify({ replaced, files: files.size }))
