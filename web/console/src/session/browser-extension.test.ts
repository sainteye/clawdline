import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- Node source tests use the source extension.
import { extensionCurrent } from "./browser-extension.ts"

test("an extension older than 0.3.0, or one that does not say, needs an update", () => {
  for (const v of ["0.3.0", "0.3.1", "0.10.0", "1.0.0"]) assert.equal(extensionCurrent(v), true, v)
  for (const v of ["0.2.0", "0.2.9", "", undefined, "test"]) assert.equal(extensionCurrent(v), false, String(v))
})

// Address navigation arrived after reload: an older loaded extension stays read-only.
test("navigation requires extension 0.5.0", () => {
  assert.equal(extensionCurrent("0.3.0", "0.5.0"), false)
  assert.equal(extensionCurrent("0.5.0", "0.5.0"), true)
})
