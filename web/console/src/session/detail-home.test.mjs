// The detail pane with no Session open, while the list is still on its way and
// once it has arrived. Rendered on the server, so what is checked is the markup
// a person would first see, not how Detail is written.
import assert from "node:assert/strict"
import { after, test } from "node:test"
import { createRequire } from "node:module"
import { fileURLToPath } from "node:url"
import { build } from "esbuild"
import React from "react"
import { renderToStaticMarkup } from "react-dom/server"

// Detail's module chain reaches the legacy page modules, which look up a few
// elements when they load; a page always has a document, this test needs a stand-in.
const element = () => ({ style: {}, dataset: {}, setAttribute() {}, appendChild() {}, addEventListener() {}, removeEventListener() {}, classList: { add() {}, remove() {}, toggle() {}, contains: () => false } })
globalThis.document ??= { getElementById: () => null, querySelector: () => null, querySelectorAll: () => [], addEventListener() {}, removeEventListener() {}, createElement: element, documentElement: { ...element(), lang: "en" }, body: element(), head: element() }
globalThis.location ??= new URL("http://synthetic.invalid/")
globalThis.window ??= { document: globalThis.document, location: globalThis.location, navigator: globalThis.navigator, matchMedia: () => ({ matches: false, addEventListener() {}, removeEventListener() {} }), addEventListener() {}, removeEventListener() {} }
globalThis.localStorage ??= { getItem: () => null, setItem() {}, removeItem() {} }
globalThis.sessionStorage ??= globalThis.localStorage

const { outputFiles } = await build({
  stdin: { contents: 'export {Detail} from "./Detail"', resolveDir: fileURLToPath(new URL(".", import.meta.url)), loader: "ts" },
  bundle: true, platform: "node", format: "cjs", packages: "external", write: false, jsx: "automatic",
  loader: { ".css": "empty", ".svg": "text", ".png": "empty" }, logLevel: "error",
  // Vite fills these in a real build; here the console runs as the local one.
  define: { "import.meta.env": "{}", "import.meta.glob": "__detailTestGlob" },
})
globalThis.__detailTestGlob = () => ({})
// The console's stores never render on a server, so they give no server
// snapshot; on this one the browser's snapshot is the first one a page shows.
const require = createRequire(import.meta.url), reactModule = require("react"), storeHook = reactModule.useSyncExternalStore
reactModule.useSyncExternalStore = (subscribe, snapshot, serverSnapshot) => storeHook(subscribe, snapshot, serverSnapshot ?? snapshot)
// The pane's overlays draw into the body; a server render has none, so they
// are drawn in place and still count as what the page shows.
require("react-dom").createPortal = (children) => children
const compiled = { exports: {} }
const importedTimers = [], nativeTimers = { setTimeout: globalThis.setTimeout, setInterval: globalThis.setInterval }
for (const kind of Object.keys(nativeTimers)) globalThis[kind] = (...args) => { const timer = nativeTimers[kind](...args); importedTimers.push(timer); return timer }
try { new Function("require", "module", "exports", outputFiles[0].text)(require, compiled, compiled.exports) }
finally { Object.assign(globalThis, nativeTimers) }
after(() => { for (const timer of importedTimers) { clearTimeout(timer); clearInterval(timer) } })
const { Detail } = compiled.exports

const render = (props) => renderToStaticMarkup(React.createElement(Detail, { row: null, onBack() {}, ...props }))
const composer = /class="composer"|<textarea|Message Claude/

test("a home screen still reading its list draws no composer and no home hero", () => {
  const html = render({ listUnknown: true })
  assert.doesNotMatch(html, composer)
  assert.doesNotMatch(html, /home-hero/)
})

test("a home screen whose list has arrived shows the home hero and no composer", () => {
  const html = render({ listUnknown: false })
  assert.match(html, /home-hero/)
  assert.doesNotMatch(html, composer)
})
