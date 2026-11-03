// The dev stack button under the Links section:
// `node --test --experimental-strip-types web/console/src/overlays/devstack-offer.test.ts`
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see `session/order.test.ts`.
import { devStackOffer, devStackWords } from "./devstack-offer.ts"
// @ts-expect-error -- a `.ts` path, for node; see `legacy/devices-bridge.test.ts`.
import { nextWord } from "../next-strings.ts"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { inCatalog } from "../catalog-testing.ts"

const lang = { documentElement: { lang: "en" } }
;(globalThis as { document?: unknown }).document = lang

const server = { kind: "server", label: "web", url: "http://127.0.0.1:3006", state: "ok", local: true }
const deploy = { kind: "deploy", label: "site", url: "https://example.com/", state: "ok", local: false }

test("a readable .devstack.json is offered to start, and a missing or broken one to set up", () => {
  assert.equal(devStackOffer("ready", [server]), "start")
  // A file that declares no ports may still name an `up` command.
  assert.equal(devStackOffer("empty", []), "start")
  assert.equal(devStackOffer("missing", [deploy]), "create")
  assert.equal(devStackOffer("attention", [deploy]), "create")
})

test("the daemon's reading wins over the rows it drew", () => {
  // A server row can be a URL another file named; only the reading says the file exists.
  assert.equal(devStackOffer("missing", [server]), "create")
  assert.equal(devStackOffer("ready", []), "start")
})

test("an older daemon without the reading falls back to whether any server row was drawn", () => {
  assert.equal(devStackOffer(undefined, [deploy, server]), "start")
  assert.equal(devStackOffer(undefined, [deploy]), "create")
  assert.equal(devStackOffer(undefined, []), "create")
})

test("each offer has its own label and request in both primary languages", () => {
  for (const tongue of ["en", "zh-Hant"] as const) {
    lang.documentElement.lang = tongue
    const said = (key: ReturnType<typeof devStackWords>["button"]) => inCatalog(tongue, () => nextWord(key))
    const start = devStackWords("start")
    const create = devStackWords("create")
    for (const key of [start.button, start.request, start.sent, create.button, create.request, create.sent]) {
      assert.notEqual(said(key), "next." + key, tongue + ": " + key + " is in the catalog")
    }
    assert.notEqual(said(start.request), said(create.request), tongue + ": starting is not setting up")
    // Starting must not become a restart of what already runs, and setting up must not start anything.
    assert.match(said(start.request), /up/, tongue + ": the start request names the file's up command")
    assert.match(said(create.request), /clawdline guide (en|zh-TW) project/, tongue + ": setup reads the guide's own contract")
  }
  lang.documentElement.lang = "en"
  assert.equal(nextWord(devStackWords("start").button), "Start dev stack")
  assert.equal(inCatalog("zh-Hant", () => nextWord(devStackWords("create").button)), "建立 dev stack")
})
