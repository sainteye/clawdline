import { strict as assert } from "node:assert"
import { test } from "node:test"
// @ts-expect-error Node's strip-types test runner needs the source extension.
import { transparencyURL } from "./public-url.ts"
test("public links retain language and supported filters without Cloud session identifiers", () => {
  const url = new URL(transparencyURL("zh-Hant", "#page=transparency&scope=default&agents=codex&session=private&machine=private"))
  assert.equal(url.origin, "https://clawdline.com")
  assert.equal(url.pathname, "/docs/transparency/")
  assert.equal(url.search, "?lang=zh-TW")
  assert.equal(url.hash, "#scope=default&agents=codex")
  assert.equal(transparencyURL("en"), "https://clawdline.com/docs/transparency/?lang=en")
  assert.equal(transparencyURL("ja"), transparencyURL("en"))
})

test("native context links retain the provider without Session or machine identity", () => {
  const url = new URL(transparencyURL("en", "#agents=antigravity&scenes=memory&session=private&machine=private"))
  assert.equal(url.hash, "#agents=antigravity&scenes=memory")
})
