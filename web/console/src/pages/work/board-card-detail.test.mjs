import assert from "node:assert/strict"
import { test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

const { outputFiles } = await build({ stdin: { contents: 'export { BoardCardDetail } from "./BoardCardDetail.tsx"; export { readWorkV2Cards } from "./api.ts"', resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" }, bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic", loader: { ".css": "empty" } })
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { BoardCardDetail } = compiled.exports
const summary = { id: "card", title: "A board card", description: "preview", summary: true }
function render(card, error = "", children = () => assert.fail("partial item reached editing controls")) {
  return renderToStaticMarkup(React.createElement(BoardCardDetail, { card, error, retry() {}, loadingLabel: "Loading full item", retryLabel: "Retry", children }))
}
test("opening a summary shows accessible loading before mounting editing controls", () => {
  const html = render(summary)
  assert.match(html, /A board card/)
  assert.match(html, /role="status"/)
  assert.match(html, /Loading full item/)
  assert.doesNotMatch(html, /<button|<input|<textarea/)
})
test("a failed detail read exposes a retry without mounting editing controls", () => {
  const html = render(summary, "Read failed")
  assert.match(html, /role="alert"/)
  assert.match(html, /Read failed/)
  assert.match(html, /<button[^>]*>Retry<\/button>/)
  assert.doesNotMatch(html, /role="status"|<input|<textarea/)
})
test("a complete item supplies its full content to editing controls", () => {
  const item = { ...summary, summary: undefined, description: "Complete description", acceptance_criteria: "Complete acceptance" }
  const html = render(item, "", (full) => React.createElement("textarea", { defaultValue: full.description + "\n" + full.acceptance_criteria }))
  assert.match(html, /Complete description\nComplete acceptance/)
  assert.doesNotMatch(html, /role="status"/)
})


test("an older local daemon gets exactly one full-list fallback with the same filters", async () => {
  const saved = globalThis.fetch
  const paths = []
  globalThis.fetch = async (path) => {
    paths.push(String(path))
    return paths.length === 1 ? new Response(JSON.stringify({ error: "bad_request", detail: "Unknown field" }), { status: 400 })
      : new Response(JSON.stringify({ rows: [{ id: "old", description: "Complete legacy content" }] }))
  }
  try {
    const answer = await compiled.exports.readWorkV2Cards("project", "done", "needle", "page-2")
    assert.equal(answer.rows[0].description, "Complete legacy content")
    assert.equal(paths.length, 2)
    assert.equal(paths[0], "/v1/work/v2/items?view=cards-v1&project=project&status=done&q=needle&cursor=page-2")
    assert.equal(paths[1], "/v1/work/v2/items?project=project&status=done&q=needle&cursor=page-2")
  } finally { globalThis.fetch = saved }
})
test("authorization failures do not amplify board reads with a fallback", async () => {
  const saved = globalThis.fetch
  let requests = 0
  globalThis.fetch = async () => {
    requests++
    return new Response(JSON.stringify({ error: "forbidden", detail: "Access denied" }), { status: 403 })
  }
  try {
    await assert.rejects(compiled.exports.readWorkV2Cards(), (error) => error.code === "forbidden")
    assert.equal(requests, 1)
  } finally { globalThis.fetch = saved }
})
