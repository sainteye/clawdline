// Whether the page is the build being served: `node --test web/console/src/build-freshness.test.ts`.
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { assetsNamed, isDifferentBuild } from "./build-freshness.ts"

const INDEX = (main: string, tokens: string) => `
  <html><head>
    <script type="module" crossorigin src="./assets/${main}"></script>
    <link rel="modulepreload" crossorigin href="./assets/${tokens}">
    <link rel="stylesheet" crossorigin href="./assets/main-4QKTFl8E.css">
  </head><body></body></html>`

const LOADED = (main: string, tokens: string) => [
  "https://app.clawdline.com/assets/" + main,
  "/assets/" + tokens,
  "./assets/main-4QKTFl8E.css",
  "https://fonts.example/none-of-our-business.css",
]

test("the same build is not a different build", () => {
  assert.equal(isDifferentBuild(INDEX("main-A.js", "tokens-B.js"), LOADED("main-A.js", "tokens-B.js")), false)
})

test("one chunk rebuilt is a different build", () => {
  assert.equal(isDifferentBuild(INDEX("main-A.js", "tokens-C.js"), LOADED("main-A.js", "tokens-B.js")), true)
})

test("an index it cannot read anything out of is no answer, not an alarm", () => {
  assert.equal(isDifferentBuild("<html><body>offline</body></html>", LOADED("main-A.js", "tokens-B.js")), false)
  assert.equal(isDifferentBuild("", LOADED("main-A.js", "tokens-B.js")), false)
})

test("a page whose own assets cannot be seen is no answer either", () => {
  assert.equal(isDifferentBuild(INDEX("main-A.js", "tokens-B.js"), []), false)
  assert.equal(isDifferentBuild(INDEX("main-A.js", "tokens-B.js"), ["https://fonts.example/x.css"]), false)
})

test("the order the tags come in does not matter, and a repeat is not a change", () => {
  const a = assetsNamed(`x assets/b-2.js y assets/a-1.js z assets/a-1.js`)
  assert.deepEqual(a, ["assets/a-1.js", "assets/b-2.js"])
})
