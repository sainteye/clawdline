// The compaction window field, both ways:
//   node --test web/console/src/pages/settings/window/compact.test.ts
import { test } from "node:test"
import assert from "node:assert/strict"
// @ts-expect-error -- a `.ts` path, for node; see session/order.test.ts.
import { COMPACT_MAX, COMPACT_MIN, compactWindowText, compactWindowValue } from "./compact.ts"

test("a value the file holds comes back from the field as itself", () => {
  for (const value of [0, COMPACT_MIN, 120_000, COMPACT_MAX]) {
    const back = compactWindowValue(compactWindowText(value))
    assert.deepEqual(back, { value }, `round trip of ${value}`)
  }
  assert.equal(compactWindowText(null), "", "absent in the file is an empty field")
})

test("an empty field, 0 and off are none", () => {
  for (const text of ["", "  ", "0", "off", "OFF"]) assert.deepEqual(compactWindowValue(text), { value: 0 }, text)
})

test("what a person types is read the way they meant it", () => {
  assert.deepEqual(compactWindowValue("200,000"), { value: 200_000 })
  assert.deepEqual(compactWindowValue("200 000"), { value: 200_000 })
  assert.deepEqual(compactWindowValue("200k"), { value: 200_000 })
  assert.deepEqual(compactWindowValue("1000K"), { value: 1_000_000 })
})

test("outside the daemon's bounds, or not a whole number, is refused before a write", () => {
  for (const text of ["49999", "1000001", "-60000", "60000.5", "lots", "1e5", "0x10000", "k"]) {
    assert.deepEqual(compactWindowValue(text), { invalid: true }, text)
  }
})
