import assert from "node:assert/strict"
import test from "node:test"
import japanese from "../../../public/catalogs/ja.json" with { type: "json" }
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { activateCatalog, resetCatalogForTest } from "../../catalog.ts"
// @ts-expect-error -- Node's strip-types test runner needs the source extension.
import { projectRefusalDetail } from "./refusal-detail.ts"

test("a copied Project status shows the matching producer detail in its true language", () => {
  const error = { code: "forbidden", detail: "Only the Project owner can change this setting." }
  assert.deepEqual(projectRefusalDetail("Cloud cannot do that. (forbidden · fixture·4)", error),
    { text: error.detail, lang: "en" })
  assert.equal(projectRefusalDetail("Reading the Project directory…", error), null)
  assert.equal(projectRefusalDetail("Only the Project owner can change this setting. (forbidden)", error), null)
  assert.equal(projectRefusalDetail("Cloud cannot do that. (unrelated_code)", error), null)
})

test("a real producer key translates the Project detail without changing its copied status", () => {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", { configurable: true, value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } } })
  try {
    assert.equal(activateCatalog(japanese, "ja"), "ja")
    assert.deepEqual(projectRefusalDetail("Cloud cannot do that. (forbidden · fixture·4)", {
      code: "forbidden", detail: "Only this machine's own token may change its settings.",
      detailKey: "http.9989084eae5cdab3",
    }), { text: japanese["http.9989084eae5cdab3"], lang: "ja" })
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
})
