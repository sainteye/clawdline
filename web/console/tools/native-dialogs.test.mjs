// Structural product guard: these calls silently cancel in the local App.
import { test } from "node:test"
import assert from "node:assert/strict"
import { readFileSync, readdirSync } from "node:fs"
import { fileURLToPath } from "node:url"
import { join, relative } from "node:path"
import ts from "typescript"

test("product decisions use the shared module instead of browser-native dialogs", () => {
  const root = fileURLToPath(new URL("../src/", import.meta.url))
  const found = []
  const walk = dir => {
    for (const entry of readdirSync(dir, { withFileTypes: true })) {
      const file = join(dir, entry.name)
      if (entry.isDirectory()) { walk(file); continue }
      if (!/\.[cm]?[jt]sx?$/.test(file) || /\.(test|e2e)\./.test(file)) continue
      const ast = ts.createSourceFile(file, readFileSync(file, "utf8"), ts.ScriptTarget.Latest, true)
      const visit = node => {
        if (ts.isCallExpression(node)) {
          const callee = node.expression
          const owner = ts.isPropertyAccessExpression(callee) || ts.isElementAccessExpression(callee) ? callee.expression : null
          const name = ts.isPropertyAccessExpression(callee) ? callee.name.text
            : ts.isElementAccessExpression(callee) && ts.isStringLiteral(callee.argumentExpression) ? callee.argumentExpression.text : ""
          if (owner && ts.isIdentifier(owner) && ["window", "globalThis", "self"].includes(owner.text) && ["confirm", "alert", "prompt"].includes(name)) {
            found.push(`${relative(root, file)}:${ast.getLineAndCharacterOfPosition(node.getStart(ast)).line + 1} ${owner.text}.${name}`)
          }
        }
        ts.forEachChild(node, visit)
      }
      visit(ast)
    }
  }
  walk(root)
  assert.deepEqual(found, [], "The local App does not provide native confirmation dialogs; use confirmAction for explicit decisions.")
})
