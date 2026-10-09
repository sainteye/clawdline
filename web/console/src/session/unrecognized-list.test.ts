import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { inCatalog } from "../catalog-testing.ts"
// @ts-expect-error -- a `.ts` path, for Node's type-strip runner.
import { nextWord } from "../next-strings.ts"

test("an unrecognized session uses a short list label and keeps the explanation in detail", () => {
  // The words follow the selected catalog since 33e1c4b1, not the document's lang.
  for (const [lang, limit] of [["zh-Hant", 6], ["en", 20]] as const) {
    const [list, detail] = inCatalog(lang, () => [nextWord("sessionStateUnrecognizedList"), nextWord("sessionStateUnrecognizedDetail")])
    assert.ok(list.length <= limit, `${lang}: list label is ${list.length} characters: ${list}`)
    assert.ok(detail.length > list.length, `${lang}: detail keeps the explanation`)
  }
})
