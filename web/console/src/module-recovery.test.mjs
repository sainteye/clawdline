import assert from "node:assert/strict"
import { test } from "node:test"
import { readFileSync } from "node:fs"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const { outputFiles } = await build({
  stdin: { contents: 'export { ModuleRecovery } from "./ModuleRecovery.tsx"; export { activateCatalog } from "./catalog.ts"',
    resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" },
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false,
  jsx: "automatic", loader: { ".css": "empty" },
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { ModuleRecovery, activateCatalog } = compiled.exports

for (const [tag, title, reload] of [
  ["en", "Part of this page could not load", "Reload"],
  ["zh-Hant", "部分頁面未能載入", "重新載入"],
]) test(`a failed module offers a visible manual recovery in ${tag}`, () => {
  const previous = globalThis.document
  globalThis.document = { documentElement: { lang: tag, dir: "ltr", setAttribute() {} } }
  try {
    activateCatalog(JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8")), tag)
    const html = renderToStaticMarkup(React.createElement(ModuleRecovery, { reload: () => assert.fail("rendering reloaded the page") }))
    assert.match(html, /role="alert"/)
    assert.ok(html.includes(title))
    assert.ok(html.includes(`>${reload}</button>`))
    assert.doesNotMatch(html, /disabled=""|hidden=""|UsageOverview|\.js|TypeError/)
  } finally { globalThis.document = previous }
})

test("a failed background page stays hidden until the person returns to it", () => {
  const html = renderToStaticMarkup(React.createElement(ModuleRecovery, { shown: false }))
  assert.match(html, /hidden=""/)
})
