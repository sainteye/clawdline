// The Bot dispatch frame's heading opens the Bot that dispatched it, by key.
import { test } from "node:test"
import assert from "node:assert/strict"
import { build } from "esbuild"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"
import { readFileSync } from "node:fs"

const { outputFiles } = await build({
  stdin: {
    contents: 'export {renderBotDispatchGroups} from "./session/bot-dispatch-groups";export {publishBotDirectory,openableBot,requestOpenBot,OPEN_BOT} from "./project-assistants/open-bot";export {activateCatalog} from "./catalog"',
    resolveDir: fileURLToPath(new URL("..", import.meta.url)), loader: "ts",
  },
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic",
  loader: { ".css": "empty" }, define: { "import.meta.env": "{}" },
})
const compiled = { exports: {} }
new Function("require", "module", "exports", outputFiles[0].text)(createRequire(import.meta.url), compiled, compiled.exports)
const { renderBotDispatchGroups, publishBotDirectory, openableBot, requestOpenBot, OPEN_BOT, activateCatalog } = compiled.exports
const catalog = tag => JSON.parse(readFileSync(new URL(`../../public/catalogs/${tag}.json`, import.meta.url), "utf8"))

const atlas = { machine_id: "studio", bot_id: "atlas" }
const summary = (key, extra = {}) => ({ bot: { key, name: "Atlas", hidden: false, ...extra }, source: {} })
const rows = [{
  id: "s1", dispatch: { name: "Atlas", dispatcher: atlas, project: { key: "p", name: "Draft Editor" } },
  render: () => React.createElement("li", { key: "s1", className: "row" }, "Session"),
}]
const heading = () => {
  const html = renderToStaticMarkup(React.createElement("ul", null, renderBotDispatchGroups(rows)))
  return /<(button|div) [^>]*class="session-list-group-heading[^"]*"[^>]*>/.exec(html)?.[0] ?? ""
}

test("a readable dispatching Bot's heading is one labelled button that opens it", () => {
  publishBotDirectory([summary(atlas)])
  globalThis.document ??= { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } }
  for (const [tag, label] of [["zh-Hant", "打開 Atlas"], ["en", "Open Atlas"]]) {
    assert.equal(activateCatalog(catalog(tag), tag), tag)
    const opening = heading()
    assert.match(opening, /^<button type="button"/)
    assert.match(opening, new RegExp(`aria-label="${label}"`))
  }
})

test("a deleted, hidden or other-machine Bot's heading stays a plain label", () => {
  for (const items of [[], [summary(atlas, { hidden: true })], [summary({ machine_id: "laptop", bot_id: "atlas" })]]) {
    publishBotDirectory(items)
    assert.match(heading(), /^<div /)
  }
  publishBotDirectory([summary(atlas)])
  const legacy = [{ ...rows[0], dispatch: { name: "Atlas" } }]
  assert.doesNotMatch(renderToStaticMarkup(React.createElement("ul", null, renderBotDispatchGroups(legacy))), /<button/)
})

test("a press asks the Bots host for exactly that Bot, and the host resolves by machine and Bot ID", () => {
  const seen = []
  const target = new EventTarget()
  globalThis.window = target
  globalThis.CustomEvent ??= class extends Event { constructor(type, init) { super(type); this.detail = init?.detail } }
  target.addEventListener(OPEN_BOT, event => seen.push(event.detail.key))
  requestOpenBot({ key: atlas })
  delete globalThis.window
  assert.deepEqual(seen, [atlas])

  const other = summary({ machine_id: "laptop", bot_id: "atlas" })
  assert.equal(openableBot([other, summary(atlas)], atlas).bot.key, atlas)
  assert.equal(openableBot([other], atlas), undefined)
  assert.equal(openableBot([summary(atlas, { hidden: true })], atlas), undefined)
})
