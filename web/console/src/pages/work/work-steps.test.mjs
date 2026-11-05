import assert from "node:assert/strict"
import { test } from "node:test"
import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const { outputFiles } = await build({ stdin: { contents: 'export { WorkSteps } from "./WorkSteps.tsx"; export { openSteps } from "./complete-item.ts"; export { activateCatalog, resetCatalogForTest } from "../../catalog.ts"', resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" }, bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic" })
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { WorkSteps, openSteps, activateCatalog, resetCatalogForTest } = compiled.exports
function inCatalog(tag, run) {
 const catalog = JSON.parse(readFileSync(new URL(`../../../public/catalogs/${tag}.json`, import.meta.url), "utf8"))
 globalThis.document = { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } }
 assert.equal(activateCatalog(catalog, tag), tag)
 try { return run() } finally { resetCatalogForTest(); Reflect.deleteProperty(globalThis, "document") }
}
const steps = [
 { id: "done", title: "完成必要修改", done: true },
 { id: "cancelled", title: "取消不再需要的處理", done: false, status: "cancelled", cancelled_at: 10, cancel_reason: "已與使用者確認不再需要" },
 { id: "pending", title: "驗證最終結果", done: false },
]
test("completed, cancelled and pending steps stay distinct in both catalogs", () => {
 for (const [tag, summary, label] of [["en", "To-dos: 1 completed · 1 cancelled · 1 pending", "Cancelled"], ["zh-Hant", "待辦：1 已完成 · 1 已取消 · 1 未處理", "已取消"]]) {
  const html = inCatalog(tag, () => renderToStaticMarkup(React.createElement(WorkSteps, { steps })))
  assert.ok(html.includes(summary))
  assert.match(html, /data-state="done"/)
  assert.match(html, /data-state="cancelled"/)
  assert.match(html, /data-state="todo"/)
  assert.ok(html.includes(label))
  assert.ok(html.includes(steps[1].cancel_reason))
  assert.match(html, /work-step-title">取消不再需要的處理/)
 }
 assert.equal(openSteps({ steps }), 1)
 assert.equal(openSteps({ steps: steps.slice(0, 2) }), 0)
 assert.equal(openSteps({ steps: [{ done: false, cancelled_at: 10 }] }), 0)
 assert.equal(openSteps({ steps: [{ done: false }, { done: true }] }), 1)
 assert.equal(renderToStaticMarkup(React.createElement(WorkSteps, { steps: [] })), "")
})
