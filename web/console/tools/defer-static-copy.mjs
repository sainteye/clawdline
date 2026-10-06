// One-shot conversion of module-level copy maps into late catalog lookups.
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const names = new Set(["STATUS", "toneWords", "STATE_WORDS", "KEPT_WORDS", "SKIP_WORDS", "iconWords", "activityWords", "MARK_WORDS", "CONDITIONS", "AUTHORITY"])
let changed = 0
function walk(directory) {
  for (const item of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, item.name)
    if (item.isDirectory()) { if (item.name !== "legacy") walk(file); continue }
    if (!/\.tsx?$/u.test(file) || /\.(test|e2e)\.tsx?$/u.test(file)) continue
    let source = fs.readFileSync(file, "utf8")
    const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
    const edits = []
    function visit(node) {
      if (ts.isVariableDeclaration(node) && names.has(node.name.getText(ast)) && node.initializer && ts.isObjectLiteralExpression(node.initializer)) {
        const object = node.initializer
        edits.push({ start: object.getStart(ast), end: object.getStart(ast), value: "localizedLiteralMap(" })
        edits.push({ start: object.end, end: object.end, value: ")" })
        function values(child) {
          if (ts.isCallExpression(child) && child.expression.getText(ast) === "catalogWord" && child.arguments.length === 2 && ts.isStringLiteral(child.arguments[0]) && child.arguments[0].text === "literal" && ts.isStringLiteral(child.arguments[1])) {
            edits.push({ start: child.getStart(ast), end: child.end, value: JSON.stringify(child.arguments[1].text) })
            return
          }
          ts.forEachChild(child, values)
        }
        values(object)
        changed++
        return
      }
      ts.forEachChild(node, visit)
    }
    visit(ast)
    if (!edits.length) continue
    for (const edit of edits.sort((a, b) => b.start - a.start || b.end - a.end)) source = source.slice(0, edit.start) + edit.value + source.slice(edit.end)
    const relative = path.relative(path.dirname(file), path.join(root, "src/catalog")).replaceAll(path.sep, "/")
    const specifier = (relative.startsWith(".") ? relative : `./${relative}`) + ".js"
    source = `import { localizedLiteralMap } from ${JSON.stringify(specifier)}\n` + source
    fs.writeFileSync(file, source)
  }
}
walk(path.join(root, "src"))
console.log(JSON.stringify({ maps: changed }))
