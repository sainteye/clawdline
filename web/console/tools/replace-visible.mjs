// One-time codemod for the frozen visible-inventory.json. It edits no copied file.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const frozen = JSON.parse(fs.readFileSync(path.join(root, "visible-inventory.json"), "utf8"))
const ids = new Map(frozen.map(({ id, value }) => [value, id]))
const normalized = (value) => value.trim().replace(/\s+/g, " ")
for (const file of new Set(frozen.map(({ file }) => file))) {
  const absolute = path.join(root, file)
  const source = fs.readFileSync(absolute, "utf8")
  const ast = ts.createSourceFile(absolute, source, ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX)
  const edits = []
  function visit(node) {
    if (ts.isJsxText(node)) {
      const raw = node.getText(ast)
      const id = ids.get(normalized(raw))
      if (id) {
        const lead = raw.match(/^\s*/u)?.[0] ?? ""
        const tail = raw.match(/\s*$/u)?.[0] ?? ""
        edits.push({ start: node.pos, end: node.end, text: `${lead}{catalogWord("inline", "${id.slice(7)}")}${tail}` })
      }
    } else if (ts.isJsxAttribute(node) && ["aria-label", "title", "placeholder", "alt"].includes(node.name.text) && node.initializer && ts.isStringLiteral(node.initializer)) {
      const id = ids.get(normalized(node.initializer.text))
      if (id) edits.push({ start: node.initializer.getStart(ast), end: node.initializer.end, text: `{catalogWord("inline", "${id.slice(7)}")}` })
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
  if (!edits.length) continue
  edits.sort((a, b) => b.start - a.start)
  let result = source
  for (const { start, end, text } of edits) result = result.slice(0, start) + text + result.slice(end)
  if (!/import\s*\{[^}]*\bcatalogWord\b[^}]*\}\s*from/u.test(source)) {
    const relative = path.relative(path.dirname(absolute), path.join(root, "src/catalog.js")).replaceAll(path.sep, "/")
    result = `import { catalogWord } from "${relative.startsWith(".") ? relative : "./" + relative}"\n` + result
  }
  fs.writeFileSync(absolute, result)
  console.log(`${file}: ${edits.length} replacements`)
}
