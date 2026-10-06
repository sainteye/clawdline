import assert from "node:assert/strict"
import { readFileSync } from "node:fs"
import test from "node:test"

const english = JSON.parse(readFileSync(new URL("../public/catalogs/en.json", import.meta.url), "utf8"))
const inventory = JSON.parse(readFileSync(new URL("../semantic-risk-keys.json", import.meta.url), "utf8"))

test("no-write and owner-only refusals remain in the semantic risk inventory", () => {
  const risk = new Set(inventory.union)
  assert.equal(inventory.englishKeys, Object.keys(english).length - 2)
  assert.ok(risk.has("legacy.webProjectNothingToLand"), "nothing to land is a no-work claim")
  for (const key of ["http.991016ce28d91e94", "http.058ea4633716f2f5", "http.88cabd3ef80ec05b"]) {
    if (Object.hasOwn(english, key)) assert.ok(risk.has(key), `${key}: refusal polarity or owner-only permission`)
  }
})
