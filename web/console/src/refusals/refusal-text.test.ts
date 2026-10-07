import { test } from "node:test"
import assert from "node:assert/strict"
import japanese from "../../public/catalogs/ja.json" with { type: "json" }
import english from "../../public/catalogs/en.json" with { type: "json" }
// @ts-expect-error -- Node runs the TypeScript source test directly.
import { activateCatalog, resetCatalogForTest } from "../catalog.ts"
// @ts-expect-error -- Node runs the TypeScript source test directly.
import { failureSentence } from "./refusal-text.ts"

test("a wire refusal uses its detail without losing the failure tag or acknowledgement", () => {
  let acknowledged = 0
  const sentence = failureSentence({
    code: "forbidden",
    detail: "The selected Project is read only.",
    detailKey: "http.0000000000000000",
    ref: { sender: "web_fixture", seq: 123 },
    acknowledge: () => { acknowledged++ },
  }, { sentence: "Generic failure", fallback: "Fallback failure" })
  assert.match(sentence, /The selected Project is read only\./u)
  assert.match(sentence, /forbidden · fixture·123/u)
  assert.doesNotMatch(sentence, /Generic failure|Fallback failure/u)
  assert.equal(acknowledged, 1)
})

test("a code-only failure keeps its screen-owned sentence", () => {
  const sentence = failureSentence({ code: "offline" }, { sentence: "This screen could not connect." })
  assert.match(sentence, /This screen could not connect\./u)
  assert.match(sentence, /offline/u)
})

test("a Cloud failure with an unknown key keeps its raw detail", () => {
  const sentence = failureSentence({
    code: "project_required",
    detail: { reason: "preserved for action logic" },
    wireDetail: "A Project must be selected.",
    detailKey: "http.0000000000000000",
  }, "Generic failure")
  assert.match(sentence, /A Project must be selected\./u)
  assert.match(sentence, /project_required/u)
  assert.doesNotMatch(sentence, /Generic failure/u)
})

test("a secondary catalog owns the frame and code-only sentence while ref and acknowledgement survive", () => {
  const oldDocument = globalThis.document
  Object.defineProperty(globalThis, "document", {
    configurable: true,
    value: { documentElement: { lang: "en", dir: "ltr", setAttribute() {} } },
  })
  try {
    assert.equal(activateCatalog(japanese, "ja"), "ja")
    let acknowledged = 0
    const sentence = failureSentence({
      code: "forbidden",
      ref: { sender: "web_fixture", seq: 123 },
      acknowledge: () => { acknowledged++ },
    })
    assert.equal(sentence, `${japanese["legacy.webFailForbidden"]}（forbidden · fixture·123）`)
    assert.equal(acknowledged, 1)

    const wire = failureSentence({ code: "unknown_wire_code", detail: "Raw producer detail." })
    assert.equal(wire, "Raw producer detail.（unknown_wire_code）")
    assert.notEqual(english["legacy.webFailWithTag"], japanese["legacy.webFailWithTag"])
  } finally {
    resetCatalogForTest()
    Object.defineProperty(globalThis, "document", { configurable: true, value: oldDocument })
  }
})
