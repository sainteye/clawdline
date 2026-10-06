// One-shot conversion of the reviewed literal inventory into catalog lookups.
import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const candidates = JSON.parse(fs.readFileSync(path.join(root, "literal-candidates.json"), "utf8"))
const inventory = JSON.parse(fs.readFileSync(path.join(root, "literal-inventory.json"), "utf8"))
const known = new Set(inventory.map((entry) => entry.id))
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
  function idFor(node, english) {
    return "literal." + crypto.createHash("sha256").update(name + "\0" + node.text + "\0" + english).digest("hex").slice(0, 12)
  }
  function lookup(id) { return `catalogWord("literal", ${JSON.stringify(id.slice(8))})` }
  function isString(node) { return ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node) }
  function visit(node) {
    if (ts.isConditionalExpression(node) && isString(node.whenTrue) && isString(node.whenFalse)) {
      const chinese = /[\u3400-\u9fff]/u.test(node.whenTrue.text) ? node.whenTrue : /[\u3400-\u9fff]/u.test(node.whenFalse.text) ? node.whenFalse : null
      const english = chinese === node.whenTrue ? node.whenFalse : node.whenTrue
      if (chinese && !/[\u3400-\u9fff]/u.test(english.text)) {
        const id = idFor(chinese, english.text)
        if (known.has(id) && candidates[id]?.include) { add(node, lookup(id)); return }
      }
    }
    if (ts.isCallExpression(node) && node.expression.getText(ast) === "words" && node.arguments.length >= 2 && isString(node.arguments[0]) && isString(node.arguments[1]) && /[\u3400-\u9fff]/u.test(node.arguments[1].text)) {
      const id = idFor(node.arguments[1], node.arguments[0].text)
      if (known.has(id) && candidates[id]?.include) { add(node, lookup(id)); return }
    }
    if (isString(node) && /[\u3400-\u9fff]/u.test(node.text) && !ts.isLiteralTypeNode(node.parent)) {
      const id = idFor(node, "")
      if (known.has(id) && candidates[id]?.include) {
        if (ts.isJsxAttribute(node.parent)) add(node, `{${lookup(id)}}`)
        else add(node, lookup(id))
        return
      }
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
  if (!edits.length) continue
  for (const edit of edits.sort((a, b) => b.start - a.start)) source = source.slice(0, edit.start) + edit.replacement + source.slice(edit.end)
  if (!source.includes('from "../catalog"') && !source.includes('from "./catalog"') && !source.includes('from "../../catalog"') && !source.includes('from "../../../catalog"')) {
    const relative = path.relative(path.dirname(file), path.join(root, "src/catalog")).replaceAll(path.sep, "/")
    const specifier = relative.startsWith(".") ? relative : `./${relative}`
    source = `import { catalogWord } from ${JSON.stringify(specifier)}\n` + source
  } else if (!source.match(/import\s*\{[^}]*\bcatalogWord\b[^}]*\}\s*from\s*["'][^"']*catalog["']/u)) {
    const relative = path.relative(path.dirname(file), path.join(root, "src/catalog")).replaceAll(path.sep, "/")
    const specifier = relative.startsWith(".") ? relative : `./${relative}`
    source = `import { catalogWord } from ${JSON.stringify(specifier)}\n` + source
  }
  fs.writeFileSync(file, source)
}
console.log(JSON.stringify({ replaced, files: files.size }))
