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

// The shape a hosted console actually has. Measured from two real builds on
// 2026-09-21 (`VITE_HOSTED_CONSOLE=… npm run build`): the index names six
// assets and the bundle holds twelve, because `CloudGate-*.js` and its
// stylesheet are reached by the dynamic `import()` in `main.tsx` and are named
// nowhere in the index. Vite's preload helper makes a `<link>` for each as it
// loads them, so they are in the page's own tags and not in the index's.
const HOSTED_INDEX = `
  <html><head>
    <script type="module" crossorigin src="./assets/main-CBCqbkb3.js"></script>
    <link rel="modulepreload" crossorigin href="./assets/tokens-zBeTDx4A.js">
    <link rel="modulepreload" crossorigin href="./assets/client-C60iIoaf.js">
    <link rel="modulepreload" crossorigin href="./assets/api-D1ZfXWCn.js">
    <link rel="stylesheet" crossorigin href="./assets/tokens-D63L1LvC.css">
    <link rel="stylesheet" crossorigin href="./assets/main-BZLaYSFB.css">
  </head><body></body></html>`

const HOSTED_LOADED = [
  "https://app.clawdline.com/assets/main-CBCqbkb3.js",
  "https://app.clawdline.com/assets/tokens-zBeTDx4A.js",
  "https://app.clawdline.com/assets/client-C60iIoaf.js",
  "https://app.clawdline.com/assets/api-D1ZfXWCn.js",
  "https://app.clawdline.com/assets/tokens-D63L1LvC.css",
  "https://app.clawdline.com/assets/main-BZLaYSFB.css",
  // Named in no index; in the page from the moment it reaches the Cloud gate.
  "https://app.clawdline.com/assets/CloudGate-C87cOfFs.js",
  "https://app.clawdline.com/assets/CloudGate-DhaPWtnD.css",
]

test("a dynamic chunk the index never names is not a new build", () => {
  assert.equal(isDifferentBuild(HOSTED_INDEX, HOSTED_LOADED), false)
})

test("a rebuild is still caught once dynamic chunks are loaded", () => {
  // The second of the two builds: one string changed inside `CloudGate.tsx`
  // alone, and the entry moved with it — `main-CBCqbkb3.js` became
  // `main-BOMcW7yy.js`, because the chunk's file name is a literal inside the
  // entry. That is what keeps the looser rule honest: a change reachable only
  // through a dynamic import still renames something the index names.
  const rebuilt = HOSTED_INDEX.replace("main-CBCqbkb3.js", "main-BOMcW7yy.js")
  assert.equal(isDifferentBuild(rebuilt, HOSTED_LOADED), true)
})

test("a page missing one asset the index names is out of date", () => {
  const short = HOSTED_LOADED.filter((url) => !url.endsWith("api-D1ZfXWCn.js"))
  assert.equal(isDifferentBuild(HOSTED_INDEX, short), true)
})
