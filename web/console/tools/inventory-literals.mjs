// Snapshot scattered Chinese literals outside the known copy tables and copied code.
import crypto from "node:crypto"
import fs from "node:fs"
import path from "node:path"
import { fileURLToPath } from "node:url"
import ts from "typescript"

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const skipped = new Set(["next-strings.ts", "pages/work/words.ts", "pages/verify/words.ts", "pages/settings/window/copy.ts", "bar/words.ts"])
const files = []
function walk(directory) {
  for (const entry of fs.readdirSync(directory, { withFileTypes: true })) {
    const file = path.join(directory, entry.name)
    if (entry.isDirectory()) {
      if (entry.name !== "legacy") walk(file)
    } else if (/\.tsx?$/u.test(file) && !/\.(test|e2e)\.tsx?$/u.test(file) && !skipped.has(path.relative(path.join(root, "src"), file))) files.push(file)
  }
}
walk(path.join(root, "src"))

const entries = []
for (const file of files.sort()) {
  const source = fs.readFileSync(file, "utf8")
  const ast = ts.createSourceFile(file, source, ts.ScriptTarget.Latest, true, file.endsWith(".tsx") ? ts.ScriptKind.TSX : ts.ScriptKind.TS)
  function visit(node) {
    if ((ts.isStringLiteral(node) || ts.isNoSubstitutionTemplateLiteral(node)) && /[\u3400-\u9fff]/u.test(node.text) && !ts.isLiteralTypeNode(node.parent)) {
      let english = ""
      const parent = node.parent
      if (ts.isConditionalExpression(parent)) {
        const other = parent.whenTrue === node ? parent.whenFalse : parent.whenTrue
        if ((ts.isStringLiteral(other) || ts.isNoSubstitutionTemplateLiteral(other)) && !/[\u3400-\u9fff]/u.test(other.text)) english = other.text
      }
      if (ts.isCallExpression(parent) && parent.expression.getText(ast) === "words") {
        const other = parent.arguments[0]
        if (other && other !== node && ts.isStringLiteral(other)) english = other.text
      }
      const value = node.text
      const localFile = path.relative(root, file)
      const id = "literal." + crypto.createHash("sha256").update(localFile + "\0" + value + "\0" + english).digest("hex").slice(0, 12)
      entries.push({ id, file: path.relative(root, file), line: ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1,
        kind: ts.SyntaxKind[parent.kind], value, english, context: source.slice(Math.max(0, node.pos - 65), Math.min(source.length, node.end + 65)).replace(/\s+/g, " ") })
    }
    ts.forEachChild(node, visit)
  }
  visit(ast)
}
if (process.argv.includes("--write")) fs.writeFileSync(path.join(root, "literal-inventory.json"), JSON.stringify(entries, null, 2) + "\n")
console.log(JSON.stringify({ occurrences: entries.length, keys: new Set(entries.map((entry) => entry.id)).size, pairedEnglish: entries.filter((entry) => entry.english).length }))
