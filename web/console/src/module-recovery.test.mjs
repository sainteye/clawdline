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
  ["en", "This page couldn’t load just now", "Reload"],
  ["zh-Hant", "這個頁面暫時未能載入", "重新載入"],
]) test(`a failed module offers a visible manual recovery in ${tag}`, () => {
  const previous = globalThis.document
  globalThis.document = { documentElement: { lang: tag, dir: "ltr", setAttribute() {} } }
  try {
    activateCatalog(JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8")), tag)
    const html = renderToStaticMarkup(React.createElement(ModuleRecovery, { reload: () => assert.fail("rendering reloaded the page") }))
    assert.match(html, /role="alert"/)
    assert.ok(html.includes(title))
    assert.ok([...html.matchAll(/<button[^>]*>([\s\S]*?)<\/button>/g)]
      .some((match) => match[1].replace(/<[^>]*>/g, "") === reload))
    assert.doesNotMatch(html, /disabled=""|hidden=""|UsageOverview|\.js|TypeError/)
  } finally { globalThis.document = previous }
})

test("a failed background page stays hidden until the person returns to it", () => {
  const html = renderToStaticMarkup(React.createElement(ModuleRecovery, { shown: false }))
  assert.match(html, /hidden=""/)
})

for (const [tag, back, alternative, manual] of [
  ["en", "Back to Sessions", "Or return to Sessions and continue your work.", "The page won’t reload automatically."],
  ["zh-Hant", "返回 Session", "也可以先返回 Session，繼續其他工作。", "頁面不會自動重新載入。"],
]) test(`only a mounted Session shell offers an in-page return in ${tag}`, () => {
  const previous = globalThis.document
  globalThis.document = { documentElement: { lang: tag, dir: "ltr", setAttribute() {} } }
  try {
    activateCatalog(JSON.parse(readFileSync(new URL(`../public/catalogs/${tag}.json`, import.meta.url), "utf8")), tag)
    const page = renderToStaticMarkup(React.createElement(ModuleRecovery, { onSessions: () => {} }))
    assert.ok(page.includes(`>${back}</button>`))
    assert.ok(page.includes(alternative))
    assert.ok(page.includes(manual))
    const startup = renderToStaticMarkup(React.createElement(ModuleRecovery, { startup: true }))
    assert.ok(!startup.includes(back))
    assert.ok(!startup.includes(alternative))
    assert.ok(startup.includes(manual))
  } finally { globalThis.document = previous }
})
